package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeLoader reads ~/.claude/projects/<dir>/<session>.jsonl files.
//
// Schema (verified against real samples on the target machine, 2026-05-18):
//
//	{
//	  "type": "user" | "assistant" | "system" | ...,
//	  "uuid": "...",
//	  "parentUuid": "..." | null,
//	  "timestamp": "2026-05-17T12:32:37.576Z",
//	  "sessionId": "...",
//	  "message": {
//	    "role": "user" | "assistant",
//	    "content": "string" | [{"type": "text" | "thinking" | "tool_use" | "tool_result", ...}, ...]
//	  }
//	}
//
// We tolerate every Claude-specific extension by capturing the *raw* line
// bytes and re-emitting them on write, only re-encoding the small set of
// fields we actually parse for the TUI.
type ClaudeLoader struct{}

// claudeLine is the minimal projection we care about. RawMessage on the
// content field lets us peek at array-vs-string without choking.
type claudeLine struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid"`
	ParentUUID string          `json:"parentUuid"`
	Timestamp  string          `json:"timestamp"`
	SessionID  string          `json:"sessionId"`
	Message    *claudeMessage  `json:"message"`
	// Permit unknown fields without erroring; json.Unmarshal ignores them.
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use id
	Name      string          `json:"name,omitempty"`       // tool name
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result link
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result body
	Input     json.RawMessage `json:"input,omitempty"`
}

// Detect returns true iff path ends in .jsonl AND the first non-empty line
// parses as a Claude-shaped object. We do not require a Claude path prefix
// so that the user can `excise testdata/foo.jsonl` outside ~/.claude.
func (l *ClaudeLoader) Detect(path string) bool {
	if !strings.HasSuffix(strings.ToLower(path), ".jsonl") {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe map[string]any
		if json.Unmarshal(line, &probe) != nil {
			return false
		}
		// minimal Claude shape: has "type" string and either "uuid" or "sessionId"
		if _, ok := probe["type"]; !ok {
			return false
		}
		_, hasUUID := probe["uuid"]
		_, hasSession := probe["sessionId"]
		_, hasParent := probe["parentUuid"]
		return hasUUID || hasSession || hasParent
	}
	return false
}

// Load reads a Claude JSONL file into a Session.
func (l *ClaudeLoader) Load(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	sess := &Session{
		Tool:       ToolClaude,
		SourcePath: path,
		SessionID:  inferSessionIDFromFilename(path),
	}

	sc := bufio.NewScanner(f)
	// Allow lines up to 16 MiB (real sessions can have very long tool inputs).
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := append([]byte(nil), sc.Bytes()...)
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var cl claudeLine
		if err := json.Unmarshal(raw, &cl); err != nil {
			// Skip unparseable lines but do not lose them — they get
			// preserved as a system-role turn so the writer can re-emit.
			sess.Turns = append(sess.Turns, Turn{
				ID:       fmt.Sprintf("line-%d", lineNo),
				Role:     RoleSystem,
				Preview:  fmt.Sprintf("[unparseable line %d, preserved verbatim]", lineNo),
				Raw:      raw,
			})
			continue
		}

		// Skip queue-operation / hook noise rows: they have no uuid and no
		// message. We still preserve them verbatim.
		if cl.UUID == "" && cl.Message == nil {
			sess.Turns = append(sess.Turns, Turn{
				ID:       fmt.Sprintf("line-%d", lineNo),
				Role:     RoleSystem,
				Preview:  fmt.Sprintf("[%s]", cl.Type),
				Raw:      raw,
			})
			continue
		}
		if sess.SessionID == "" && cl.SessionID != "" {
			sess.SessionID = cl.SessionID
		}

		t := Turn{
			ID:        cl.UUID,
			ParentID:  cl.ParentUUID,
			Role:      mapClaudeRole(cl.Type, cl.Message),
			Timestamp: parseClaudeTime(cl.Timestamp),
			Raw:       raw,
		}
		if t.ID == "" {
			t.ID = fmt.Sprintf("line-%d", lineNo)
		}

		text, calls, results := extractClaudeBlocks(cl.Message)
		t.Preview = previewText(text)
		t.TokenEst = estimateTokens(text) + len(calls)*8
		t.ToolCalls = calls
		t.ToolResults = results
		sess.Turns = append(sess.Turns, t)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	SortByTimestamp(sess.Turns)
	return sess, nil
}

func mapClaudeRole(typ string, msg *claudeMessage) Role {
	if msg != nil {
		switch msg.Role {
		case "user":
			return RoleUser
		case "assistant":
			return RoleAssistant
		}
	}
	switch typ {
	case "user":
		return RoleUser
	case "assistant":
		return RoleAssistant
	case "tool_result", "tool":
		return RoleTool
	}
	return RoleSystem
}

func parseClaudeTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// extractClaudeBlocks pulls (text, toolCalls, toolResults) from a Claude
// message.content, which may be a string OR an array of typed blocks.
func extractClaudeBlocks(msg *claudeMessage) (string, []ToolCall, []ToolResult) {
	if msg == nil || len(msg.Content) == 0 {
		return "", nil, nil
	}
	// Try string first.
	var asStr string
	if err := json.Unmarshal(msg.Content, &asStr); err == nil {
		return asStr, nil, nil
	}
	var blocks []claudeContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return string(msg.Content), nil, nil
	}
	var textParts []string
	var calls []ToolCall
	var results []ToolResult
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				textParts = append(textParts, b.Text)
			}
		case "thinking":
			// We intentionally skip thinking from the preview — the user
			// rarely cares about it when picking turns to cut, and it
			// blows out the preview.
		case "tool_use":
			calls = append(calls, ToolCall{ID: b.ID, Name: b.Name})
			if b.Name != "" {
				textParts = append(textParts, "[tool_use: "+b.Name+"]")
			}
		case "tool_result":
			results = append(results, ToolResult{ToolUseID: b.ToolUseID})
			// tool_result content can itself be a string or array of blocks.
			textParts = append(textParts, extractToolResultText(b.Content))
		}
	}
	return strings.Join(textParts, " "), calls, results
}

func extractToolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return "[tool_result] " + s
	}
	var arr []claudeContentBlock
	if err := json.Unmarshal(raw, &arr); err == nil {
		parts := []string{"[tool_result]"}
		for _, b := range arr {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return "[tool_result]"
}

func inferSessionIDFromFilename(path string) string {
	base := filepath.Base(path)
	if i := strings.LastIndex(base, "."); i > 0 {
		return base[:i]
	}
	return base
}

// ClaudeWriter implements Writer for Claude JSONL sessions. It re-emits each
// surviving turn's Raw bytes verbatim, separated by single newlines, and
// performs an atomic snapshot-then-tmp-then-rename to preserve the
// invariants documented on the package.
type ClaudeWriter struct{}

// Write enforces invariant 4 (atomic) for Claude sessions. The caller is
// responsible for any snapshot strategy (see internal/safety).
func (w *ClaudeWriter) Write(s *Session) error {
	if s == nil {
		return errors.New("nil session")
	}
	if s.Tool != ToolClaude {
		return fmt.Errorf("ClaudeWriter cannot write %s sessions", s.Tool)
	}
	if s.SourcePath == "" {
		return errors.New("session has no source path")
	}
	dir := filepath.Dir(s.SourcePath)
	tmp, err := os.CreateTemp(dir, ".excise-*.jsonl.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	bw := bufio.NewWriter(tmp)
	if err := emitClaudeTurns(bw, s.Turns); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := bw.Flush(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, s.SourcePath); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func emitClaudeTurns(w io.Writer, turns []Turn) error {
	for _, t := range turns {
		raw := bytes.TrimRight(t.Raw, "\r\n")
		if len(raw) == 0 {
			continue
		}
		if _, err := w.Write(raw); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	return nil
}
