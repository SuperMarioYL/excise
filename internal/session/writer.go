package session

import "fmt"

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
