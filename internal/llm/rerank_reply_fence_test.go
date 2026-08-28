package llm

import (
	"errors"
	"testing"
)

// fix-rerank-reply-uppercase-json-fence: parseRerankReply used to strip a
// leading fence with case-sensitive strings.TrimPrefix(raw, "```json"), so a
// ```JSON fence (several local Ollama models) left "JSON\n[...]" which
// json.Unmarshal rejected -> ErrLLMUnavailable -> the LLM rerank was silently
// discarded despite the model returning a valid verdict. These tests pin the
// fix: the opening fence and an optional language tag are stripped
// case-insensitively before JSON parsing.

func TestParseRerankReplyFenceCaseInsensitive(t *testing.T) {
	body := `[{"turn_id":"u-032","score":9.5,"reason":"kept regenerating the same buggy hunk"}]`
	cases := []struct {
		name string
		raw  string
	}{
		{name: "uppercase JSON fence", raw: "```JSON\n" + body + "\n```"},
		{name: "lowercase json fence", raw: "```json\n" + body + "\n```"},
		{name: "mixed-case Json fence", raw: "```Json\n" + body + "\n```"},
		{name: "javascript tag", raw: "```javascript\n" + body + "\n```"},
		{name: "bare fence, no tag", raw: "```\n" + body + "\n```"},
		{name: "fence with trailing spaces on tag line", raw: "```json   \n" + body + "\n```"},
		{name: "no fence, bare array", raw: body},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRerankReply(c.raw)
			if err != nil {
				t.Fatalf("parseRerankReply(%q) failed: %v", c.raw, err)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 reply, got %d", len(got))
			}
			if got[0].TurnID != "u-032" || got[0].Score != 9.5 {
				t.Errorf("parsed reply = %+v, want u-032/9.5", got[0])
			}
			if got[0].Reason == "" {
				t.Errorf("reason must be preserved, got empty")
			}
		})
	}
}

// TestParseRerankReplyUppercaseFenceUsedToFail is the headline regression: an
// uppercase ```JSON fence used to return ErrLLMUnavailable (the verdict was
// silently dropped). After the fix it must parse cleanly.
func TestParseRerankReplyUppercaseFenceUsedToFail(t *testing.T) {
	raw := "```JSON\n[{\"turn_id\":\"t1\",\"score\":1.0,\"reason\":\"bad\"}]\n```"
	got, err := parseRerankReply(raw)
	if err != nil {
		if errors.Is(err, ErrLLMUnavailable) {
			t.Fatalf("BUG REGRESSION: uppercase ```JSON fence defeated parsing (verdict discarded): %v", err)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].TurnID != "t1" {
		t.Fatalf("expected t1 parsed, got %+v", got)
	}
}

// TestParseRerankReplyResultsWrapper still works (the {"results":[...]}
// alternate shape) and a missing closing fence no longer crashes.
func TestParseRerankReplyResultsWrapperAndTruncated(t *testing.T) {
	// {"results":[...]} wrapper path unchanged.
	wrapped := `{"results":[{"turn_id":"t1","score":2.0,"reason":"x"}]}`
	got, err := parseRerankReply(wrapped)
	if err != nil {
		t.Fatalf("results-wrapper parse failed: %v", err)
	}
	if len(got) != 1 || got[0].TurnID != "t1" {
		t.Errorf("wrapper parse = %+v", got)
	}
	// A truncated fence (opening ``` but no closing) must fall through to the
	// plain-array parse path without panicking; garbage still errors with the
	// sentinel so callers fall back to the heuristic ranking.
	if _, err := parseRerankReply("```json\nnot json at all"); err == nil {
		t.Errorf("garbage inside a fence must still error so the caller falls back")
	}
}
