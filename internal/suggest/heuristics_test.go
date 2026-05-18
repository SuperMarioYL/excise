package suggest

import (
	"testing"

	"github.com/SuperMarioYL/excise/internal/session"
)

// mkTurn is a tiny helper to keep test fixtures concise.
func mkTurn(id string, role session.Role, tokens int, preview string) session.Turn {
	return session.Turn{ID: id, Role: role, TokenEst: tokens, Preview: preview}
}

func TestDetectHighTokenCost(t *testing.T) {
	turns := []session.Turn{
		mkTurn("u1", session.RoleUser, 5000, "huge user message that we ignore"),
		mkTurn("a1", session.RoleAssistant, 1999, "just below threshold"),
		mkTurn("a2", session.RoleAssistant, 2000, "exactly at threshold"),
		mkTurn("a3", session.RoleAssistant, 8000, "well above threshold"),
		mkTurn("t1", session.RoleTool, 3000, "tool result big"),
	}
	got := detectHighTokenCost(turns)
	if _, ok := got["u1"]; ok {
		t.Errorf("user turn should not be flagged for high_token_cost")
	}
	if _, ok := got["a1"]; ok {
		t.Errorf("turn below threshold should not be flagged")
	}
	if _, ok := got["a2"]; !ok {
		t.Errorf("turn at threshold should fire")
	}
	if _, ok := got["a3"]; !ok {
		t.Errorf("8000-token turn should fire")
	}
	if got["a3"][0].Score <= got["a2"][0].Score {
		t.Errorf("score should scale with tokens; a3=%.2f a2=%.2f", got["a3"][0].Score, got["a2"][0].Score)
	}
	if _, ok := got["t1"]; !ok {
		t.Errorf("tool turn at 3000 tokens should fire")
	}
}

// claudeEditTurn returns a Claude-shaped raw line carrying a single Edit
// tool_use to the given file path.
func claudeEditTurn(id, path string) session.Turn {
	raw := []byte(`{"type":"assistant","uuid":"` + id + `","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"editing"},` +
		`{"type":"tool_use","id":"toolu_` + id + `","name":"Edit","input":{"file_path":"` + path + `","old_string":"a","new_string":"b"}}` +
		`]}}`)
	return session.Turn{
		ID:        id,
		Role:      session.RoleAssistant,
		Raw:       raw,
		ToolCalls: []session.ToolCall{{ID: "toolu_" + id, Name: "Edit"}},
	}
}

func TestDetectRepeatedFileEdit(t *testing.T) {
	turns := []session.Turn{
		claudeEditTurn("a1", "foo.py"),
		claudeEditTurn("a2", "foo.py"),
		claudeEditTurn("a3", "foo.py"),
		claudeEditTurn("a4", "bar.py"), // breaks the streak
	}
	got := detectRepeatedFileEdit(turns)
	for _, id := range []string{"a1", "a2", "a3"} {
		if _, ok := got[id]; !ok {
			t.Errorf("expected %s flagged for repeated_file_edit", id)
		}
	}
	if _, ok := got["a4"]; ok {
		t.Errorf("a4 (different file) should not be flagged")
	}
}

func TestDetectRepeatedFileEditUnderThreshold(t *testing.T) {
	turns := []session.Turn{
		claudeEditTurn("a1", "foo.py"),
		claudeEditTurn("a2", "foo.py"),
	}
	got := detectRepeatedFileEdit(turns)
	if len(got) != 0 {
		t.Errorf("under threshold should not fire; got %d flags", len(got))
	}
}

func TestDetectUserCorrectionFollowsUp(t *testing.T) {
	turns := []session.Turn{
		mkTurn("a1", session.RoleAssistant, 500, "I refactored it"),
		mkTurn("u1", session.RoleUser, 5, "no, try a different approach"),
		mkTurn("a2", session.RoleAssistant, 500, "OK how about this"),
		mkTurn("u2", session.RoleUser, 5, "looks great thanks"),
	}
	got := detectUserCorrectionFollowsUp(turns)
	if _, ok := got["a1"]; !ok {
		t.Errorf("a1 should be flagged (next user said correction)")
	}
	if _, ok := got["a2"]; ok {
		t.Errorf("a2 should not be flagged (no correction follows)")
	}
}

func TestDetectUserCorrectionFollowsUpChinese(t *testing.T) {
	turns := []session.Turn{
		mkTurn("a1", session.RoleAssistant, 500, "I made changes"),
		mkTurn("u1", session.RoleUser, 5, "不对，换个思路"),
	}
	got := detectUserCorrectionFollowsUp(turns)
	if _, ok := got["a1"]; !ok {
		t.Errorf("Chinese correction phrase should flag a1, got %+v", got)
	}
}

// errorToolResultTurn manufactures a tool_use → tool_result(error) → user-correction
// triple. We pre-fill Raw so looksLikeError can find the error marker.
func TestDetectToolUseErrorFollowedByCorrection(t *testing.T) {
	turns := []session.Turn{
		{
			ID:        "a1",
			Role:      session.RoleAssistant,
			ToolCalls: []session.ToolCall{{ID: "tc1", Name: "Edit"}},
			Raw:       []byte(`{"type":"assistant","uuid":"a1"}`),
		},
		{
			ID:          "u1",
			Role:        session.RoleUser,
			ToolResults: []session.ToolResult{{ToolUseID: "tc1"}},
			Raw:         []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc1","content":"Error: file not found","is_error":true}]}}`),
			Preview:     "Error: file not found",
		},
		{
			ID:      "u2",
			Role:    session.RoleUser,
			Preview: "that is wrong, undo that",
			Raw:     []byte(`{"type":"user","message":{"role":"user","content":"that is wrong, undo that"}}`),
		},
	}
	got := detectToolUseErrorFollowedByCorrection(turns)
	if _, ok := got["a1"]; !ok {
		t.Fatalf("a1 should fire tool_use_error_then_correction, got %+v", got)
	}
}

func TestDetectToolUseErrorNoCorrection(t *testing.T) {
	turns := []session.Turn{
		{ID: "a1", Role: session.RoleAssistant, ToolCalls: []session.ToolCall{{ID: "tc1", Name: "Edit"}}},
		{ID: "u1", Role: session.RoleUser, ToolResults: []session.ToolResult{{ToolUseID: "tc1"}}, Preview: "Error: nope"},
		{ID: "u2", Role: session.RoleUser, Preview: "thanks anyway, that's fine"},
	}
	got := detectToolUseErrorFollowedByCorrection(turns)
	if _, ok := got["a1"]; ok {
		t.Errorf("a1 should not fire (no correction lexicon match in u2)")
	}
}

func TestDetectLongDriftNoToolCalls(t *testing.T) {
	turns := []session.Turn{
		{ID: "u1", Role: session.RoleUser},
		{ID: "a1", Role: session.RoleAssistant}, // no tool_calls
		{ID: "a2", Role: session.RoleAssistant},
		{ID: "a3", Role: session.RoleAssistant},
		{ID: "a4", Role: session.RoleAssistant},
		{ID: "a5", Role: session.RoleAssistant},
		{ID: "u2", Role: session.RoleUser}, // breaks the run
		{ID: "a6", Role: session.RoleAssistant},
	}
	got := detectLongDriftNoToolCalls(turns)
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		if _, ok := got[id]; !ok {
			t.Errorf("%s should be flagged (drift of 5)", id)
		}
	}
	if _, ok := got["a6"]; ok {
		t.Errorf("a6 (lone assistant after break) should not fire")
	}
}

func TestDetectLongDriftBrokenByToolCall(t *testing.T) {
	turns := []session.Turn{
		{ID: "a1", Role: session.RoleAssistant},
		{ID: "a2", Role: session.RoleAssistant},
		{ID: "a3", Role: session.RoleAssistant, ToolCalls: []session.ToolCall{{ID: "x", Name: "Bash"}}}, // breaks
		{ID: "a4", Role: session.RoleAssistant},
		{ID: "a5", Role: session.RoleAssistant},
	}
	got := detectLongDriftNoToolCalls(turns)
	if len(got) != 0 {
		t.Errorf("no run reached threshold; got %+v", got)
	}
}

func TestLexiconMatchShortTokens(t *testing.T) {
	// "no" should match standalone but not inside "note".
	if m := LexiconMatch("no, that's wrong"); m.Phrase != "wrong" && m.Phrase != "no" && m.Phrase != "that's wrong" {
		// either is acceptable; we just want SOMETHING to fire
		if m.Weight == 0 {
			t.Errorf("expected a match, got %+v", m)
		}
	}
	if m := LexiconMatch("I made a note about this"); m.Phrase == "no" {
		t.Errorf("'no' should not match inside 'note'; got %+v", m)
	}
}

func TestLexiconMatchEmpty(t *testing.T) {
	if m := LexiconMatch(""); m.Weight != 0 {
		t.Errorf("empty input should not match; got %+v", m)
	}
}
