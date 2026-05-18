package session

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// cursorBubbleRow is one row from cursorDiskKV with the composerId parsed
// out of the key.
type cursorBubbleRow struct {
	composer string
	bubble   string
	value    json.RawMessage
}

// readCursorBubbles invokes the system `sqlite3` CLI to enumerate every
// bubble row in cursorDiskKV. We use JSON mode so multi-line values do not
// confuse line-based parsing.
//
// If sqlite3 is missing from PATH, we return a clear error pointing the
// user at the fixture path.
func readCursorBubbles(dbPath string) ([]cursorBubbleRow, error) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, fmt.Errorf("sqlite3 CLI not found on PATH (needed to read Cursor's state.vscdb); install sqlite3 or pass a fixture .jsonl file instead: %w", err)
	}
	cmd := exec.Command(
		"sqlite3",
		"-json",
		"-readonly",
		dbPath,
		"SELECT key, value FROM cursorDiskKV WHERE key LIKE 'bubbleId:%'",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 read failed (Cursor may be holding the database open; close Cursor and retry): %w", err)
	}
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		return nil, nil
	}
	var rows []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("decode sqlite3 json: %w", err)
	}
	result := make([]cursorBubbleRow, 0, len(rows))
	for _, r := range rows {
		// Cursor stores `value` as a sqlite TEXT column whose payload is
		// already-JSON-encoded — sqlite3 -json double-encodes it as a JSON
		// string. Unwrap.
		var asStr string
		if err := json.Unmarshal(r.Value, &asStr); err == nil {
			r.Value = json.RawMessage(asStr)
		}
		parts := strings.SplitN(r.Key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, cursorBubbleRow{
			composer: parts[1],
			bubble:   parts[2],
			value:    r.Value,
		})
	}
	return result, nil
}
