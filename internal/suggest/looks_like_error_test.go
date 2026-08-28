package suggest

import (
	"testing"
)

// fix-looks-like-error-is-error-spacing: looksLikeError used to substring-
// match the compact `"is_error":true` (no space) only. Real Claude Code
// tool_results — and excise's own testdata/claude_session_polluted.jsonl
// line 20 — use standard spaced JSON `"is_error": true`. When the result
// content also lacked the words error/failed/exception/traceback, the
// tool_use_error_then_correction heuristic silently dropped the failing
// tool_use turn — a false negative. These tests pin the fix: the
// is_error / isError flag is honoured regardless of JSON spacing.

func TestLooksLikeErrorIsErrorFlagSpacing(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "spaced is_error true, no error-word in content",
			raw:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc1","content":"[Errno 2] No such file or directory: 'config.yaml'","is_error": true}]}}`,
			want: true,
		},
		{
			name: "compact is_error true, no error-word in content",
			raw:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc1","content":"[Errno 2] No such file","is_error":true}]}}`,
			want: true,
		},
		{
			name: "spaced isError true",
			raw:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc1","content":"boom","isError": true}]}}`,
			want: true,
		},
		{
			name: "extra spaces around colon",
			raw:  `{"content":"boom" , "is_error"  :  true}`,
			want: true,
		},
		{
			name: "is_error false must not match",
			raw:  `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok","is_error": false}]}}`,
			want: false,
		},
		{
			name: "error word still matches via the permissive regex path",
			raw:  `{"type":"user","message":{"content":[{"type":"tool_result","content":"Error: file not found"}]}}`,
			want: true,
		},
		{
			name: "no error signal at all",
			raw:  `{"type":"user","message":{"content":"all good"}}`,
			want: false,
		},
		{
			name: "empty bytes",
			raw:  ``,
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := looksLikeError([]byte(c.raw))
			if got != c.want {
				t.Errorf("looksLikeError(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

// TestLooksLikeErrorNoFalsePositiveOnIsErrorKeyInString guards against an
// over-broad regex: the flag must require `true`, so an is_error key whose
// value is a string must not trigger.
func TestLooksLikeErrorNoFalsePositiveOnIsErrorKeyInString(t *testing.T) {
	raw := `{"note":"the field is_error: true is documented","is_error":"maybe"}`
	if looksLikeError([]byte(raw)) {
		t.Errorf("looksLikeError must not fire on an is_error string value or prose mentioning the field")
	}
}
