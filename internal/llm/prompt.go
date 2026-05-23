package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/SuperMarioYL/excise/internal/session"
	"github.com/SuperMarioYL/excise/internal/suggest"
)

// previewMaxRunes caps how much of each turn we send to the LLM. Sending
// the full transcript would defeat the entire purpose (cost + leakage).
// 320 runes is enough for the model to recognise the pattern; the heuristic
// triggers already named *why* the turn was shortlisted.
const previewMaxRunes = 320

// renderRerankPrompt builds the prompt sent to Ollama. The shape is JSON-in
// / JSON-out by design: we ask the model to emit `{"turn_id":..., "score":...,
// "reason":...}` records and parse them back. Asking for prose first then
// re-extracting would be flakier and we'd lose the deterministic fallback.
//
// The prompt is INTENTIONALLY hard-coded for v0.3. The "let the user override
// the prompt" knob is in the v0.3 out-of-scope list — we don't yet know
// which prompt works well across the polluted-session distribution and
// shipping flexibility before we know would lock in a bad baseline.
func renderRerankPrompt(s *session.Session, shortlist []suggest.TurnScore) string {
	var b strings.Builder
	b.WriteString(`You are an expert reviewer of long Claude Code / Cursor agent sessions.

A heuristic pre-filter has shortlisted the turns below as candidates for surgical
removal ("excision") from the session. Your job is to RERANK them — highest
score = most worth cutting first — and write ONE short reason per turn
explaining why (or, if a turn should NOT be cut, why the heuristic was wrong).

Reply with a JSON array, nothing else. Each element MUST have:
  - "turn_id"  (string, copy verbatim from the input)
  - "score"    (number; higher = cut sooner; you may invert from the heuristic)
  - "reason"   (string; one sentence, <= 120 chars)

Do NOT add prose before or after the array. Do NOT wrap in markdown fences.

Shortlist:
`)
	for i, ts := range shortlist {
		preview := truncateRunes(strings.TrimSpace(ts.Preview), previewMaxRunes)
		triggers := suggest.TriggerSummary(ts)
		// Use a compact, parser-friendly delimiter so the model doesn't get
		// confused by a turn whose preview itself contains JSON.
		fmt.Fprintf(&b, "\n--- turn #%d ---\nturn_id: %s\nrole: %s\ntokens: %d\nheuristic_triggers: %s\npreview: %q\n",
			i+1, ts.TurnID, ts.Role, ts.Tokens, triggers, preview)
	}
	b.WriteString("\nReturn only the JSON array.\n")
	return b.String()
}

// rerankReply is the parsed model output (one element per shortlist turn).
type rerankReply struct {
	TurnID string  `json:"turn_id"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// parseRerankReply decodes the model's JSON-array response. The decoder is
// deliberately strict — anything other than a clean array of the expected
// shape returns ErrLLMUnavailable so the caller falls back to the heuristic
// ranking. We accept the JSON either bare or wrapped in a ```json fence
// (some models stubbornly emit fences).
func parseRerankReply(raw string) ([]rerankReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty rerank reply", ErrLLMUnavailable)
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}
	// Some models return {"results":[...]} instead of a bare array.
	// Accept either.
	var arr []rerankReply
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}
	var wrap struct {
		Results []rerankReply `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &wrap); err == nil && len(wrap.Results) > 0 {
		return wrap.Results, nil
	}
	return nil, fmt.Errorf("%w: could not parse rerank reply as JSON array", ErrLLMUnavailable)
}

func truncateRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
