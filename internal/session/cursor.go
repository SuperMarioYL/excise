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

// CursorLoader reads Cursor's chat data.
//
// Cursor stores composers and individual chat bubbles inside
//
//	~/Library/Application Support/Cursor/User/globalStorage/state.vscdb
//
// (linux: ~/.config/Cursor/...; windows: %APPDATA%/Cursor/...).
//
// Two table layouts exist:
//
//   1. The live `state.vscdb` (sqlite) — `cursorDiskKV` table — keys of shape
//      `bubbleId:<composerId>:<bubbleId>` whose value is a JSON blob with
//      fields like `text`, `type` (numeric), `bubbleId`, `toolResults`,
//      `tokenCount`, etc. This is the format verified against the real
//      install on this machine (Cursor build 2025-04).
//
//   2. A fixture-only JSON-lines export, used for unit tests and for users
//      who do not have Cursor installed locally. Each line is one bubble
//      JSON object plus an envelope:
//
//        {"composerId": "...", "bubble": { ...the same value as above... }}
//
//      Detection: file extension is .jsonl AND first non-empty line has a
//      top-level "bubble" object.
//
// We deliberately do NOT add a CGO sqlite driver because the dependency cost
// (and Linux/Windows cross-compile friction) outweighs the benefit for v0.1.
// Instead we shell out to the `sqlite3` CLI for sqlite reads. If sqlite3 is
// not on PATH, Load returns a clear error pointing the user at the fixture
// path.
//
// IMPORTANT: this is read-only for sqlite in v0.1. Cursor's state.vscdb is
// frequently held open by the Cursor process; mutating it from the outside
// while Cursor is running risks corruption. The writer therefore refuses to
// write directly back to state.vscdb and instead emits a side-car .excised
// file the user can manually copy back (or, in v0.2, we will add a
// Cursor-must-be-closed safety prompt).
type CursorLoader struct{}

type cursorBubble struct {
	BubbleID    string          `json:"bubbleId"`
	Type        int             `json:"type"`
	Text        string          `json:"text"`
	Richtext    string          `json:"richtext,omitempty"`
	ToolResults []cursorTool    `json:"toolResults,omitempty"`
	ToolFormers []cursorTool    `json:"toolFormers,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"`
	TokenCount  cursorTokenCnt  `json:"tokenCount,omitempty"`
	// permit unknown fields
	Extra map[string]json.RawMessage `json:"-"`
}

type cursorTool struct {
	ID   string `json:"toolCallId"`
	Name string `json:"name"`
}

type cursorTokenCnt struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type cursorEnvelope struct {
	ComposerID string          `json:"composerId"`
	Bubble     json.RawMessage `json:"bubble"`
}

// Detect returns true for either format.
func (l *CursorLoader) Detect(path string) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".vscdb") || strings.HasSuffix(lower, ".sqlite") {
		return true
	}
	if !strings.HasSuffix(lower, ".jsonl") {
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
		_, hasBubble := probe["bubble"]
		_, hasComposer := probe["composerId"]
		return hasBubble && hasComposer
	}
	return false
}

// Load picks the sqlite or jsonl branch by suffix.
func (l *CursorLoader) Load(path string) (*Session, error) {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".vscdb") || strings.HasSuffix(lower, ".sqlite") {
		return l.loadSqlite(path)
	}
	return l.loadJSONL(path)
}

func (l *CursorLoader) loadJSONL(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sess := &Session{
		Tool:       ToolCursor,
		SourcePath: path,
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := append([]byte(nil), sc.Bytes()...)
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var env cursorEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			sess.Turns = append(sess.Turns, Turn{
				ID:      fmt.Sprintf("line-%d", lineNo),
				Role:    RoleSystem,
				Preview: fmt.Sprintf("[unparseable line %d, preserved verbatim]", lineNo),
				Raw:     raw,
			})
			continue
		}
		if sess.ComposerID == "" {
			sess.ComposerID = env.ComposerID
			sess.SessionID = env.ComposerID
		}
		var b cursorBubble
		if err := json.Unmarshal(env.Bubble, &b); err != nil {
			sess.Turns = append(sess.Turns, Turn{
				ID:      fmt.Sprintf("line-%d", lineNo),
				Role:    RoleSystem,
				Preview: fmt.Sprintf("[unparseable bubble line %d]", lineNo),
				Raw:     raw,
			})
			continue
		}
		sess.Turns = append(sess.Turns, bubbleToTurn(&b, raw, lineNo))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	SortByTimestamp(sess.Turns)
	return sess, nil
}

// loadSqlite returns a graceful "no Cursor data found" error if the database
// has no bubble rows, but otherwise extracts every bubble row as a Turn.
//
// We deliberately do not enumerate composers — the v0.1 CLI requires the
// caller to first pick a composer (or we pick the newest). Multi-composer
// rendering is v0.2.
func (l *CursorLoader) loadSqlite(path string) (*Session, error) {
	rows, err := readCursorBubbles(path)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no Cursor bubble rows in %s (state.vscdb may be empty or held open by Cursor)", path)
	}
	// pick the composer with the most bubbles; deterministic on ties.
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.composer]++
	}
	bestComposer := ""
	bestN := -1
	for c, n := range counts {
		if n > bestN || (n == bestN && c < bestComposer) {
			bestN = n
			bestComposer = c
		}
	}
	sess := &Session{
		Tool:       ToolCursor,
		SourcePath: path,
		ComposerID: bestComposer,
		SessionID:  bestComposer,
	}
	for _, r := range rows {
		if r.composer != bestComposer {
			continue
		}
		var b cursorBubble
		if err := json.Unmarshal(r.value, &b); err != nil {
			continue
		}
		sess.Turns = append(sess.Turns, bubbleToTurn(&b, r.value, 0))
	}
	if len(sess.Turns) == 0 {
		return nil, errors.New("no parseable bubbles for the largest composer")
	}
	SortByTimestamp(sess.Turns)
	return sess, nil
}

func bubbleToTurn(b *cursorBubble, raw []byte, lineNo int) Turn {
	id := b.BubbleID
	if id == "" {
		id = fmt.Sprintf("bubble-%d", lineNo)
	}
	role := RoleAssistant
	if b.Type == 1 {
		role = RoleUser
	}
	if len(b.ToolResults) > 0 && strings.TrimSpace(b.Text) == "" {
		role = RoleTool
	}
	text := b.Text
	if text == "" && b.Richtext != "" {
		text = b.Richtext
	}
	calls := make([]ToolCall, 0, len(b.ToolFormers))
	for _, t := range b.ToolFormers {
		calls = append(calls, ToolCall{ID: t.ID, Name: t.Name})
	}
	results := make([]ToolResult, 0, len(b.ToolResults))
	for _, t := range b.ToolResults {
		results = append(results, ToolResult{ToolUseID: t.ID})
	}
	tok := b.TokenCount.InputTokens + b.TokenCount.OutputTokens
	if tok == 0 {
		tok = estimateTokens(text)
	}
	ts := time.Time{}
	if b.Timestamp != "" {
		ts, _ = time.Parse(time.RFC3339Nano, b.Timestamp)
	}
	return Turn{
		ID:          id,
		Role:        role,
		Timestamp:   ts,
		Preview:     previewText(text),
		TokenEst:    tok,
		ToolCalls:   calls,
		ToolResults: results,
		Raw:         raw,
	}
}

// CursorWriter writes Cursor sessions. For sqlite sources it emits a side-car
// `.excised.jsonl` next to the database; for jsonl sources it does an
// atomic snapshot-then-tmp-then-rename, matching ClaudeWriter.
type CursorWriter struct{}

func (w *CursorWriter) Write(s *Session) error {
	if s == nil {
		return errors.New("nil session")
	}
	if s.Tool != ToolCursor {
		return fmt.Errorf("CursorWriter cannot write %s sessions", s.Tool)
	}
	lower := strings.ToLower(s.SourcePath)
	if strings.HasSuffix(lower, ".vscdb") || strings.HasSuffix(lower, ".sqlite") {
		return w.writeSidecar(s)
	}
	return w.writeJSONL(s)
}

func (w *CursorWriter) writeSidecar(s *Session) error {
	out := s.SourcePath + ".excised.jsonl"
	tmp, err := os.CreateTemp(filepath.Dir(out), ".excise-*.jsonl.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	bw := bufio.NewWriter(tmp)
	if err := emitCursorEnvelopes(bw, s); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	_ = bw.Flush()
	tmp.Close()
	return os.Rename(tmpPath, out)
}

func (w *CursorWriter) writeJSONL(s *Session) error {
	dir := filepath.Dir(s.SourcePath)
	tmp, err := os.CreateTemp(dir, ".excise-*.jsonl.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	bw := bufio.NewWriter(tmp)
	for _, t := range s.Turns {
		raw := bytes.TrimRight(t.Raw, "\r\n")
		if len(raw) == 0 {
			continue
		}
		if _, err := bw.Write(raw); err != nil {
			tmp.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		_, _ = bw.Write([]byte("\n"))
	}
	_ = bw.Flush()
	tmp.Close()
	return os.Rename(tmpPath, s.SourcePath)
}

func emitCursorEnvelopes(w io.Writer, s *Session) error {
	for _, t := range s.Turns {
		env := cursorEnvelope{ComposerID: s.ComposerID, Bubble: json.RawMessage(t.Raw)}
		line, err := dumpJSONLine(env)
		if err != nil {
			return err
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	return nil
}

// DefaultCursorPath returns the platform-specific path to state.vscdb.
func DefaultCursorPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// macOS canonical path. Linux/Windows fallbacks are best-effort.
	candidates := []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("no Cursor state.vscdb found in %v", candidates)
}
