package suggest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/SuperMarioYL/excise/internal/session"
)

// Heuristic identifiers — surfaced as short tags in suggest output.
const (
	HHighTokenCost                    = "high_token_cost"
	HRepeatedFileEdit                 = "repeated_file_edit"
	HUserCorrectionFollowsUp          = "user_correction_follows_up"
	HToolUseErrorFollowedByCorrection = "tool_use_error_then_correction"
	HLongDriftNoToolCalls             = "long_drift_no_tool_calls"
)

// HeuristicTrigger is one (heuristic_id, score_contribution, reason) tuple.
// A single turn can have multiple triggers attached.
type HeuristicTrigger struct {
	ID     string  `json:"id"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Tunables — kept package-level so tests can pin them and the locked plan
// numbers (token_threshold=2000, window=3, drift=5) are visible at a glance.
const (
	HighTokenCostThreshold = 2000
	RepeatedFileWindow     = 3
	RepeatedFileThreshold  = 3
	LongDriftThreshold     = 5
)

// editingToolNames is the set of tool names that we treat as "edits the file
// at a given path". Claude Code uses Edit/Write/MultiEdit; Cursor uses
// edit_file. The set is intentionally small and explicit — false negatives
// (a custom tool not on the list) just means RepeatedFileEdit won't fire,
// which is safe (worst case: we under-suggest).
var editingToolNames = map[string]bool{
	"Edit":       true,
	"Write":      true,
	"MultiEdit":  true,
	"edit_file":  true,
	"write_file": true,
}

// errorMarker recognises the conventional `is_error: true` payload or a
// leading "ERROR" / "Error: " prefix that Claude Code surfaces in tool
// results. We keep the test deliberately permissive — false positives here
// only mean the next user correction is double-attributed, which the
// downstream scorer already deduplicates.
var errorMarker = regexp.MustCompile(`(?i)\b(error|failed|exception|traceback)\b`)

// detectHighTokenCost flags assistant or tool turns whose TokenEst meets the
// threshold. The threshold is a property of the trigger, not the turn — a
// 500-token turn alone never flags, but a 500-token turn plus a correction
// follow-up will still be flagged via the second heuristic.
func detectHighTokenCost(turns []session.Turn) map[string][]HeuristicTrigger {
	out := map[string][]HeuristicTrigger{}
	for _, t := range turns {
		if t.TokenEst < HighTokenCostThreshold {
			continue
		}
		// Only assistant / tool turns are interesting; user turns are cheap
		// and removing them rarely buys back tokens.
		if t.Role != session.RoleAssistant && t.Role != session.RoleTool {
			continue
		}
		// score scales gently with token weight: a 4000-token turn scores
		// roughly 2× a 2000-token turn, but bounded to keep one giant turn
		// from dominating the ranking.
		raw := float64(t.TokenEst) / float64(HighTokenCostThreshold)
		if raw > 3.0 {
			raw = 3.0
		}
		out[t.ID] = append(out[t.ID], HeuristicTrigger{
			ID:     HHighTokenCost,
			Score:  0.6 + 0.4*raw, // 1.0 at threshold, up to 1.8 at 3× threshold
			Reason: fmt.Sprintf("turn weighs ~%d tokens (threshold %d)", t.TokenEst, HighTokenCostThreshold),
		})
	}
	return out
}

// detectRepeatedFileEdit walks the turn slice and counts consecutive edits
// to the same file path within a sliding window. When the count meets the
// threshold, every contributing turn is flagged.
//
// We extract file paths from tool_use inputs by parsing the raw JSON of each
// turn (the loader stripped the input bytes but the raw line is preserved).
// We accept both Claude's `file_path` field and Cursor's `path` field.
func detectRepeatedFileEdit(turns []session.Turn) map[string][]HeuristicTrigger {
	out := map[string][]HeuristicTrigger{}

	// Build a per-turn list of edited paths.
	type edit struct {
		turnID string
		path   string
	}
	var edits []edit
	for _, t := range turns {
		paths := extractEditPaths(t)
		for _, p := range paths {
			edits = append(edits, edit{turnID: t.ID, path: p})
		}
	}
	if len(edits) < RepeatedFileThreshold {
		return out
	}

	// Sliding window: count consecutive edits to the same path. A path's
	// count is reset on any other-path edit. When count ≥ threshold, the
	// last N contributing turn ids are flagged.
	curPath := ""
	curIDs := []string{}
	flag := func(ids []string, path string) {
		// dedupe — same turn id may appear twice if it has two MultiEdit blocks
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			out[id] = append(out[id], HeuristicTrigger{
				ID:    HRepeatedFileEdit,
				Score: 1.2,
				Reason: fmt.Sprintf("%d+ consecutive edits to %s within window %d",
					RepeatedFileThreshold, shortPath(path), RepeatedFileWindow),
			})
		}
	}
	for _, e := range edits {
		if e.path != curPath {
			curPath = e.path
			curIDs = []string{e.turnID}
			continue
		}
		curIDs = append(curIDs, e.turnID)
		if len(curIDs) > RepeatedFileWindow {
			curIDs = curIDs[len(curIDs)-RepeatedFileWindow:]
		}
		if len(curIDs) >= RepeatedFileThreshold {
			flag(curIDs, curPath)
		}
	}
	return out
}

// detectUserCorrectionFollowsUp scans for any user turn whose text matches
// the correction lexicon, then flags the IMMEDIATELY PRECEDING assistant /
// tool turn (the one being corrected).
func detectUserCorrectionFollowsUp(turns []session.Turn) map[string][]HeuristicTrigger {
	out := map[string][]HeuristicTrigger{}
	for i, t := range turns {
		if t.Role != session.RoleUser {
			continue
		}
		match := LexiconMatch(t.Preview)
		if match.Weight == 0 {
			// Preview may be truncated to 120 chars; fall back to scanning
			// the raw payload too, but only if we have it.
			match = LexiconMatch(extractTextFromRaw(t.Raw))
		}
		if match.Weight == 0 {
			continue
		}
		// Find the most-recent non-user predecessor (skip prior user turns).
		for j := i - 1; j >= 0; j-- {
			prev := turns[j]
			if prev.Role == session.RoleUser {
				continue
			}
			out[prev.ID] = append(out[prev.ID], HeuristicTrigger{
				ID:    HUserCorrectionFollowsUp,
				Score: 0.8 + 0.6*match.Weight, // 0.8–1.4
				Reason: fmt.Sprintf("user replied with correction phrase %q (weight %.2f)",
					match.Phrase, match.Weight),
			})
			break
		}
	}
	return out
}

// detectToolUseErrorFollowedByCorrection flags any tool_use turn whose
// downstream tool_result contains an error marker AND whose next user turn
// matches the correction lexicon. The flagged turn id is the tool_use turn
// (the one that issued the failing call).
func detectToolUseErrorFollowedByCorrection(turns []session.Turn) map[string][]HeuristicTrigger {
	out := map[string][]HeuristicTrigger{}

	// Map tool_use ids back to their owner turn id, and tool_use_id back to
	// the result turn that contains it.
	ownerOfUse := map[string]string{} // tool_use_id -> owner turn id
	resultOfUse := map[string]int{}   // tool_use_id -> result turn index
	for i, t := range turns {
		for _, c := range t.ToolCalls {
			if c.ID != "" {
				ownerOfUse[c.ID] = t.ID
			}
		}
		for _, r := range t.ToolResults {
			if r.ToolUseID != "" {
				resultOfUse[r.ToolUseID] = i
			}
		}
	}

	for toolUseID, ownerID := range ownerOfUse {
		resIdx, ok := resultOfUse[toolUseID]
		if !ok {
			continue
		}
		resTurn := turns[resIdx]
		// Check the raw payload for an error marker. Fall back to preview.
		if !looksLikeError(resTurn.Raw) && !errorMarker.MatchString(resTurn.Preview) {
			continue
		}
		// Find the next user turn AFTER resIdx and test its text against
		// the correction lexicon.
		for j := resIdx + 1; j < len(turns); j++ {
			if turns[j].Role != session.RoleUser {
				continue
			}
			text := turns[j].Preview
			match := LexiconMatch(text)
			if match.Weight == 0 {
				match = LexiconMatch(extractTextFromRaw(turns[j].Raw))
			}
			if match.Weight == 0 {
				break // first user turn after the error did not correct → bail
			}
			out[ownerID] = append(out[ownerID], HeuristicTrigger{
				ID:    HToolUseErrorFollowedByCorrection,
				Score: 1.3,
				Reason: fmt.Sprintf("tool_use %s returned error and user replied %q",
					shortToolID(toolUseID), match.Phrase),
			})
			break
		}
	}
	return out
}

// detectLongDriftNoToolCalls flags assistant turns inside any run of N+
// consecutive assistant turns that contain ZERO tool_use blocks. The classic
// "let me summarize / let me think out loud" rabbit-hole.
func detectLongDriftNoToolCalls(turns []session.Turn) map[string][]HeuristicTrigger {
	out := map[string][]HeuristicTrigger{}
	run := []int{}
	flush := func() {
		if len(run) < LongDriftThreshold {
			run = run[:0]
			return
		}
		for _, idx := range run {
			out[turns[idx].ID] = append(out[turns[idx].ID], HeuristicTrigger{
				ID:    HLongDriftNoToolCalls,
				Score: 0.9,
				Reason: fmt.Sprintf("part of a %d-turn assistant drift with no tool calls (threshold %d)",
					len(run), LongDriftThreshold),
			})
		}
		run = run[:0]
	}
	for i, t := range turns {
		if t.Role != session.RoleAssistant {
			flush()
			continue
		}
		if len(t.ToolCalls) > 0 {
			flush()
			continue
		}
		run = append(run, i)
	}
	flush()
	return out
}

// extractEditPaths pulls file-path arguments from any tool_use block in a
// turn whose tool name is in editingToolNames. We parse t.Raw as JSON and
// walk the message.content blocks; for Cursor we walk the bubble.toolFormers
// (which don't carry input args in the v0.1 fixture, so we fall back to
// scanning the bubble.text for `path=...` markers).
func extractEditPaths(t session.Turn) []string {
	if len(t.ToolCalls) == 0 && t.Role != session.RoleAssistant {
		return nil
	}
	if len(t.Raw) == 0 {
		return nil
	}
	// Try Claude Code shape first.
	var claude struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(t.Raw, &claude); err == nil && len(claude.Message.Content) > 0 {
		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(claude.Message.Content, &blocks) == nil {
			var paths []string
			for _, b := range blocks {
				if b.Type != "tool_use" {
					continue
				}
				if !editingToolNames[b.Name] {
					continue
				}
				if p := pathFromToolInput(b.Input); p != "" {
					paths = append(paths, p)
				}
			}
			if len(paths) > 0 {
				return paths
			}
		}
	}
	// Try Cursor shape. The Cursor jsonl loader stores the ENVELOPE
	// {"bubble":{...}} in t.Raw, but the sqlite loader (loadSqlite) stores
	// the BARE bubble value with no envelope wrapper — see
	// internal/session/cursor.go:217 (bubbleToTurn(&b, r.value, 0), where
	// r.value is the unwrapped bubble JSON). So we must accept both shapes:
	// unwrap the envelope first, and if that yields an empty bubble
	// (bare-bubble shape), re-parse t.Raw directly at the top level. Without
	// this the Cursor branch unmarshals into an empty Bubble, finds no
	// toolFormers/text, and returns nil — so RepeatedFileEdit never fires on
	// the production sqlite path.
	var env struct {
		Bubble cursorBubbleHeuristic `json:"bubble"`
	}
	if err := json.Unmarshal(t.Raw, &env); err == nil {
		if paths := pathsFromCursorBubble(env.Bubble); len(paths) > 0 {
			return paths
		}
		// Envelope unwrap yielded an empty bubble → bare-bubble shape
		// (the sqlite path). Re-parse t.Raw directly as the bubble value.
		if env.Bubble.Text == "" && len(env.Bubble.ToolFormers) == 0 {
			var bare cursorBubbleHeuristic
			if err := json.Unmarshal(t.Raw, &bare); err == nil {
				if paths := pathsFromCursorBubble(bare); len(paths) > 0 {
					return paths
				}
			}
		}
	}
	return nil
}

// cursorBubbleHeuristic is the subset of a Cursor bubble the heuristic layer
// re-parses from t.Raw. toolFormers may carry the edited file path in either
// `input` or `args`; bubble.text is a last-resort source of `path=...` markers
// for hand-written polluted demos. Shared by the envelope and bare-bubble
// parse paths so the sqlite regression cannot diverge from the jsonl path.
type cursorBubbleHeuristic struct {
	Text        string `json:"text"`
	ToolFormers []struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
		Args  json.RawMessage `json:"args"`
	} `json:"toolFormers"`
}

// pathsFromCursorBubble walks a Cursor bubble's toolFormers for edited file
// paths, falling back to a `path=` text marker in the bubble text. Used by
// both the envelope unwrap and the bare-bubble re-parse in extractEditPaths.
func pathsFromCursorBubble(b cursorBubbleHeuristic) []string {
	var paths []string
	for _, f := range b.ToolFormers {
		if !editingToolNames[f.Name] {
			continue
		}
		if p := pathFromToolInput(f.Input); p != "" {
			paths = append(paths, p)
			continue
		}
		if p := pathFromToolInput(f.Args); p != "" {
			paths = append(paths, p)
		}
	}
	// Fallback: bubble.text occasionally contains "path=foo.go" markers
	// in our fixtures so users can write polluted demos by hand.
	if len(paths) == 0 && b.Text != "" {
		if p := pathFromTextMarker(b.Text); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// pathFromToolInput returns the file_path / path / target_file value from a
// tool input JSON blob, or "" if none.
func pathFromToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, k := range []string{"file_path", "path", "target_file", "filePath", "filename"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

var pathMarker = regexp.MustCompile(`(?i)\b(?:path|file)=([^\s,)]+)`)

func pathFromTextMarker(s string) string {
	m := pathMarker.FindStringSubmatch(s)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// looksLikeError scans the raw tool_result payload for the conventional
// {"is_error": true} flag or "error" / "failed" tokens. Permissive on
// purpose — false positives only widen heuristic-3's reach, never cause
// data loss.
func looksLikeError(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	if errorMarker.Match(raw) {
		return true
	}
	if strings.Contains(string(raw), `"is_error":true`) || strings.Contains(string(raw), `"isError":true`) {
		return true
	}
	return false
}

// extractTextFromRaw is a best-effort plaintext extractor — used when the
// 120-char preview was too short to catch a correction phrase.
func extractTextFromRaw(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var claude struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &claude) == nil && len(claude.Message.Content) > 0 {
		var asStr string
		if json.Unmarshal(claude.Message.Content, &asStr) == nil {
			return asStr
		}
		var blocks []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(claude.Message.Content, &blocks) == nil {
			var parts []string
			for _, b := range blocks {
				if b.Text != "" {
					parts = append(parts, b.Text)
				}
				// tool_result content can be a plain string
				if b.Type == "tool_result" && len(b.Content) > 0 {
					var s string
					if json.Unmarshal(b.Content, &s) == nil {
						parts = append(parts, s)
					}
				}
			}
			return strings.Join(parts, " ")
		}
	}
	// Try Cursor shape. The jsonl loader stores the ENVELOPE
	// {"bubble":{"text":...}} in t.Raw, but the sqlite loader stores the
	// BARE bubble value with no envelope wrapper (internal/session/cursor.go:217).
	// Accept both so the correction-phrase fallback works on the production
	// Cursor sqlite path, not just the jsonl fixture path — otherwise phrases
	// beyond the 120-char preview are silently missed on the real install.
	var env struct {
		Bubble struct {
			Text string `json:"text"`
		} `json:"bubble"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Bubble.Text != "" {
		return env.Bubble.Text
	}
	var bare struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &bare) == nil && bare.Text != "" {
		return bare.Text
	}
	return ""
}

func shortPath(p string) string {
	if len(p) <= 40 {
		return p
	}
	return "…" + p[len(p)-39:]
}

func shortToolID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}
