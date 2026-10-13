package session

import (
	"fmt"
	"os"
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

// preserveFileMode copies the permission bits of dest (when it already
// exists) onto tmp before the atomic rename. os.CreateTemp creates mode
// 0600, so without this every atomic write silently tightened the
// transcript's file mode; a first-time write (no destination yet) falls
// back to the conventional 0644. Called after the tmp file is closed and
// before os.Rename; a failure here must abort the write so the guarantee
// stays honest rather than silently degrading to 0600 again.
func preserveFileMode(tmpPath, destPath string) error {
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(destPath); err == nil {
		perm = fi.Mode().Perm()
	}
	return os.Chmod(tmpPath, perm)
}
