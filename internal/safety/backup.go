// Package safety implements the snapshot, edit-log, and rollback machinery
// that turns Excise from a "hope you backed up" knife into a "git for your
// agent transcript" pair of operations.
//
// On every commit, BeforeWrite() copies the source file (sqlite databases are
// not snapshotted in v0.1 — see cursor.go for why) into
//
//	~/.excise/snapshots/<session-id>/<rfc3339-timestamp>.jsonl.gz
//
// Snapshots older than 30 days are pruned on the next BeforeWrite call so the
// directory does not grow unbounded.
//
// Every commit also appends a JSON-line to
//
//	~/.excise/edit_log.jsonl
//
// with { ts, session_id, source_path, snapshot, removed_ids, command }
// so a user can audit every cut they have ever made.
package safety

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	snapshotRetention = 30 * 24 * time.Hour
	snapshotsSubdir   = "snapshots"
	editLogFile       = "edit_log.jsonl"
)

// Root returns ~/.excise, creating it if needed.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".excise")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

// Snapshot is one persisted backup.
type Snapshot struct {
	ID         string    // <session-id>/<timestamp>
	SessionID  string
	Path       string    // absolute path to the .jsonl.gz file
	SourcePath string    // original source path captured at snapshot time
	CreatedAt  time.Time
}

// BeforeWrite snapshots `sourcePath` for `sessionID` and returns the
// Snapshot. It also prunes snapshots older than 30 days. Safe to call even
// if sourcePath does not exist (returns an error but does not panic).
func BeforeWrite(sessionID, sourcePath string) (*Snapshot, error) {
	if sessionID == "" {
		sessionID = "unknown"
	}
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, snapshotsSubdir, sanitize(sessionID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	pruneOld(dir)

	src, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open source for snapshot: %w", err)
	}
	defer src.Close()

	ts := time.Now().UTC().Format("2006-01-02T15-04-05.000Z")
	snapPath := filepath.Join(dir, ts+".jsonl.gz")
	out, err := os.Create(snapPath)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, src); err != nil {
		gz.Close()
		out.Close()
		_ = os.Remove(snapPath)
		return nil, err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		_ = os.Remove(snapPath)
		return nil, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(snapPath)
		return nil, err
	}
	return &Snapshot{
		ID:         filepath.Join(sanitize(sessionID), ts),
		SessionID:  sessionID,
		Path:       snapPath,
		SourcePath: sourcePath,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// LogEdit appends one entry to ~/.excise/edit_log.jsonl. Best-effort: errors
// are returned but do not block the caller.
func LogEdit(entry map[string]any) error {
	root, err := Root()
	if err != nil {
		return err
	}
	path := filepath.Join(root, editLogFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(entry)
}

// ListSnapshots returns every snapshot, newest first.
func ListSnapshots() ([]Snapshot, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, snapshotsSubdir)
	var out []Snapshot
	err = filepath.WalkDir(base, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".jsonl.gz") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		parts := strings.SplitN(rel, string(os.PathSeparator), 2)
		if len(parts) != 2 {
			return nil
		}
		sessionID := parts[0]
		tsStr := strings.TrimSuffix(parts[1], ".jsonl.gz")
		ts, _ := time.Parse("2006-01-02T15-04-05.000Z", tsStr)
		out = append(out, Snapshot{
			ID:        filepath.Join(sessionID, tsStr),
			SessionID: sessionID,
			Path:      p,
			CreatedAt: ts,
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Rollback restores `snapshotID` to `dest`. dest may be empty, in which case
// we attempt to read the matching `source_path` from the edit log; if that
// fails we return an error and require the caller to specify --to.
func Rollback(snapshotID, dest string) error {
	root, err := Root()
	if err != nil {
		return err
	}
	src := filepath.Join(root, snapshotsSubdir, snapshotID+".jsonl.gz")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("snapshot %s not found at %s", snapshotID, src)
	}
	if dest == "" {
		dest, err = lookupSourcePath(snapshotID)
		if err != nil {
			return fmt.Errorf("cannot infer destination for %s; pass --to <path>: %w", snapshotID, err)
		}
	}
	if dest == "" {
		return fmt.Errorf("no destination path; pass --to <path>")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".excise-rollback-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, gz); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// lookupSourcePath scans the edit log for the most recent entry that
// references snapshotID and returns its source_path.
func lookupSourcePath(snapshotID string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(root, editLogFile)
	f, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var found string
	for {
		var entry map[string]any
		if err := dec.Decode(&entry); err != nil {
			break
		}
		snap, _ := entry["snapshot"].(string)
		// snapshot field may be stored as the absolute path; compare both
		// the id and the path's tail.
		if snap == snapshotID || strings.HasSuffix(snap, snapshotID+".jsonl.gz") {
			if src, ok := entry["source_path"].(string); ok {
				found = src
			}
		}
	}
	return found, nil
}

func pruneOld(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-snapshotRetention)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-' || c == '_' || c == '.':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
