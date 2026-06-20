// Package session loads, mutates, and writes coding-agent transcripts.
//
// The package defines a tool-agnostic Turn / Session model plus a single
// Excise(set<turn_id>) operation that preserves four invariants:
//
//  1. Removing a turn that owns tool_use blocks removes the paired
//     tool_result turns.
//  2. Removing a tool_result turn requires (or warns about) removing the
//     originating tool_use turn.
//  3. The ordering of surviving turns is preserved; their stable IDs are
//     preserved as well.
//  4. Writes are atomic: snapshot first, write to a tmp file, rename.
//
// Two concrete loaders are provided: claude.go (Claude Code JSONL) and
// cursor.go (Cursor state.vscdb sqlite).
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tool identifies which coding-agent format a Session originated from.
type Tool string

const (
	ToolClaude  Tool = "claude"
	ToolCursor  Tool = "cursor"
	ToolUnknown Tool = "unknown"
)

// Role is the speaker of a single Turn. We collapse Claude's `system` /
// `tool_result` types and Cursor's numeric types down to a small enum so the
// TUI does not need to know about format-specific edge cases.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"   // a turn whose payload is purely a tool_result
	RoleSystem    Role = "system" // meta / system / queue rows
)

// ToolCall represents a single tool_use block owned by a Turn.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ToolResult represents a single tool_result block owned by a Turn.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
}

// Turn is one logical entry in a transcript. ID is stable across edits.
//
// Raw holds the original on-disk JSON line (Claude) or sqlite value (Cursor)
// so writer.go can re-emit untouched bytes for surviving turns and avoid
// lossy round-trips through our model.
type Turn struct {
	ID          string // stable id (Claude uuid; Cursor bubbleId)
	ParentID    string // parentUuid / parentBubbleId, "" if root
	Role        Role
	Timestamp   time.Time
	Preview     string       // first ~120 chars of textual content
	TokenEst    int          // very rough token estimate (chars / 4)
	ToolCalls   []ToolCall   // tool_use blocks owned by this turn
	ToolResults []ToolResult // tool_result blocks owned by this turn
	Raw         []byte       // verbatim original payload
	Meta        any          // format-specific metadata (loader's own struct)
}

// Session is an ordered list of Turn plus the metadata needed to write it
// back to disk in the same format it was loaded from.
type Session struct {
	Tool       Tool
	SourcePath string // file path (Claude) or sqlite path (Cursor)
	ComposerID string // Cursor only: bubble group id
	SessionID  string // Claude only: session uuid; Cursor: composer uuid
	Turns      []Turn
}

// Loader is the contract every format reader satisfies.
type Loader interface {
	Detect(path string) bool
	Load(path string) (*Session, error)
}

// Writer is the contract every format writer satisfies. The implementation
// is responsible for honoring the atomic-write invariant.
type Writer interface {
	Write(s *Session) error
}

// Auto picks a Loader by sniffing path / extension. Falls back to Claude.
func Auto(path string) (Loader, error) {
	cl := &ClaudeLoader{}
	cu := &CursorLoader{}
	if cu.Detect(path) {
		return cu, nil
	}
	if cl.Detect(path) {
		return cl, nil
	}
	return nil, fmt.Errorf("could not detect tool format for %s (supported: claude jsonl, cursor vscdb)", path)
}

// LoadAuto is a convenience helper for the CLI.
func LoadAuto(path string) (*Session, error) {
	ld, err := Auto(path)
	if err != nil {
		return nil, err
	}
	return ld.Load(path)
}

// LoadWithTool forces a specific loader.
func LoadWithTool(tool Tool, path string) (*Session, error) {
	switch tool {
	case ToolClaude:
		return (&ClaudeLoader{}).Load(path)
	case ToolCursor:
		return (&CursorLoader{}).Load(path)
	default:
		return LoadAuto(path)
	}
}

// DiscoverNewestClaude returns the most recently-modified Claude Code session
// jsonl under ~/.claude/projects/, searching across every project directory.
//
// It is the zero-arg invocation backbone of `excise`.
func DiscoverNewestClaude() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".claude", "projects")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("no Claude projects directory at %s", root)
	}

	type cand struct {
		path string
		mod  time.Time
	}
	var best cand
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		fi, ferr := d.Info()
		if ferr != nil {
			return nil
		}
		if fi.ModTime().After(best.mod) {
			best = cand{path: p, mod: fi.ModTime()}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if best.path == "" {
		return "", fmt.Errorf("no .jsonl session files under %s", root)
	}
	return best.path, nil
}

// estimateTokens is a very rough heuristic. We do not import a tokenizer
// because (a) the user has already paid the inference cost and we are not
// billing them, and (b) accuracy matters less than transparency — the header
// just gives the user a sense of "did this cut save anything meaningful."
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// 4 chars per token is the rule-of-thumb for English; we are happy
	// to be 30% off in either direction.
	return (len(s) + 3) / 4
}

// previewText shortens text to about 120 chars on a single line.
func previewText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// collapse whitespace
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
		if b.Len() >= 120 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	// Cap by rune count, not byte offset: byte-slicing a string that contains
	// multibyte UTF-8 (common for the zh-CN-first user base) can split a rune
	// and emit invalid UTF-8 into both the TUI and the Ollama rerank prompt.
	if r := []rune(out); len(r) > 117 {
		out = string(r[:117]) + "..."
	}
	return out
}

// SortByTimestamp ensures a stable display order. We rely on it after loading
// any format to make the TUI consistent.
func SortByTimestamp(turns []Turn) {
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].Timestamp.Equal(turns[j].Timestamp) {
			return turns[i].ID < turns[j].ID
		}
		return turns[i].Timestamp.Before(turns[j].Timestamp)
	})
}

// dumpJSONLine encodes v as a single JSON line with no HTML-escaping.
// Used by the writer when round-tripping a turn whose Raw bytes we want to
// re-emit cleanly.
func dumpJSONLine(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := []byte(b.String())
	// json.Encoder appends a newline; the caller wants that.
	return out, nil
}
