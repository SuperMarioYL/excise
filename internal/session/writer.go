package session

import (
	"fmt"
	"strings"
)

// WriterFor returns the right Writer implementation for the session's tool.
// We keep this separate from each loader file so the CLI can do a single
// dispatch.
func WriterFor(s *Session) (Writer, error) {
	switch s.Tool {
	case ToolClaude:
		return &ClaudeWriter{}, nil
	case ToolCursor:
		return &CursorWriter{}, nil
	default:
		return nil, fmt.Errorf("no writer for tool %q", s.Tool)
	}
}

// WriteTarget returns the file path the session's Writer will actually mutate.
// For most sources this is SourcePath itself, but a Cursor sqlite source is
// non-destructive: CursorWriter emits a sidecar <db>.excised.jsonl rather than
// touching the live state.vscdb. Callers (snapshot/rollback bookkeeping) must
// use this — not SourcePath — so rollback never targets the live database for a
// sidecar-only cut.
func WriteTarget(s *Session) string {
	if s == nil {
		return ""
	}
	if s.Tool == ToolCursor {
		lower := strings.ToLower(s.SourcePath)
		if strings.HasSuffix(lower, ".vscdb") || strings.HasSuffix(lower, ".sqlite") {
			return s.SourcePath + ".excised.jsonl"
		}
	}
	return s.SourcePath
}
