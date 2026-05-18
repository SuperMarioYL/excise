// Package suggest implements Excise's v0.2 heuristic suggestion engine.
//
// The engine is a pure stdlib, zero-network function from a single Session
// to a ranked list of TurnScore. It applies 5 hand-rolled heuristics:
//
//  1. HighTokenCost                — assistant turn weighing ≥ 2000 tokens
//  2. RepeatedFileEdit             — same file path edited 3+ times in a window
//  3. UserCorrectionFollowsUp      — next user turn matches the correction lexicon
//  4. ToolUseErrorFollowedByCorrection — tool returned error and user corrected
//  5. LongDriftNoToolCalls         — 5+ consecutive assistant turns with no tool_use
//
// No LLM. No network. No state crosses sessions (the scorer is a pure
// function). The package never modifies the input Session — it only reads.
package suggest

import (
	"strings"
	"unicode"
)

// CorrectionPhrase is one hand-curated correction marker plus its confidence
// weight. Weight is in [0,1]; higher weight means the phrase is a more
// reliable "the previous turn went wrong" signal in isolation.
type CorrectionPhrase struct {
	Phrase string
	Weight float64
}

// CorrectionLexicon is the merged en + zh-CN curated list of correction
// markers. It is exported so tests can pin behavior and so future tuning
// is a single-file change.
//
// Entries are ordered from short / generic / low-weight to long / specific /
// high-weight so the scanner can stop on the first high-weight hit.
var CorrectionLexicon = []CorrectionPhrase{
	// --- English ---
	{Phrase: "no", Weight: 0.30},
	{Phrase: "wrong", Weight: 0.45},
	{Phrase: "nope", Weight: 0.50},
	{Phrase: "actually", Weight: 0.55},
	{Phrase: "instead", Weight: 0.55},
	{Phrase: "revert", Weight: 0.65},
	{Phrase: "undo that", Weight: 0.75},
	{Phrase: "forget that", Weight: 0.75},
	{Phrase: "scratch that", Weight: 0.80},
	{Phrase: "let me try", Weight: 0.60},
	{Phrase: "try again", Weight: 0.60},
	{Phrase: "try a different approach", Weight: 0.90},
	{Phrase: "different approach", Weight: 0.80},
	{Phrase: "go back", Weight: 0.60},
	{Phrase: "that's wrong", Weight: 0.85},
	{Phrase: "that is wrong", Weight: 0.85},
	{Phrase: "not what i asked", Weight: 0.85},
	{Phrase: "that's not right", Weight: 0.80},
	{Phrase: "stop doing that", Weight: 0.85},

	// --- 简体中文 ---
	{Phrase: "不对", Weight: 0.70},
	{Phrase: "不是", Weight: 0.40},
	{Phrase: "重来", Weight: 0.80},
	{Phrase: "算了", Weight: 0.70},
	{Phrase: "回退", Weight: 0.75},
	{Phrase: "撤销", Weight: 0.75},
	{Phrase: "换个思路", Weight: 0.90},
	{Phrase: "再试一次", Weight: 0.60},
	{Phrase: "不要这样", Weight: 0.80},
	{Phrase: "走错了", Weight: 0.80},
	{Phrase: "搞错了", Weight: 0.75},
	{Phrase: "停下", Weight: 0.65},
}

// MatchedPhrase is the result of LexiconMatch — the highest-weight phrase
// that fired in the given text plus that weight.
type MatchedPhrase struct {
	Phrase string
	Weight float64
}

// LexiconMatch scans `text` for any phrase in CorrectionLexicon. It returns
// the highest-weight match. If nothing matched, weight is 0 and Phrase is "".
//
// The matcher is intentionally simple: case-insensitive substring on a
// whitespace-normalized lowercase copy of the text. We add word-boundary
// guarding only for the shortest (and noisiest) English tokens like "no"
// to avoid matching "note" / "now" / "knot" etc. Chinese phrases match as
// raw substrings (no boundary check; Chinese has no spaces).
func LexiconMatch(text string) MatchedPhrase {
	if text == "" {
		return MatchedPhrase{}
	}
	norm := normalizeForMatch(text)
	best := MatchedPhrase{}
	for _, e := range CorrectionLexicon {
		p := strings.ToLower(e.Phrase)
		if !containsAsToken(norm, p) {
			continue
		}
		if e.Weight > best.Weight {
			best = MatchedPhrase{Phrase: e.Phrase, Weight: e.Weight}
		}
	}
	return best
}

// AllLexiconMatches returns every distinct phrase that fired, sorted by
// descending weight. Used by heuristics that want to surface multiple
// triggers in a single user reply.
func AllLexiconMatches(text string) []MatchedPhrase {
	if text == "" {
		return nil
	}
	norm := normalizeForMatch(text)
	var out []MatchedPhrase
	seen := map[string]bool{}
	for _, e := range CorrectionLexicon {
		p := strings.ToLower(e.Phrase)
		if seen[p] {
			continue
		}
		if !containsAsToken(norm, p) {
			continue
		}
		seen[p] = true
		out = append(out, MatchedPhrase{Phrase: e.Phrase, Weight: e.Weight})
	}
	// simple insertion sort by weight desc; the list is tiny.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Weight > out[j-1].Weight; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// normalizeForMatch collapses whitespace and lowercases, but preserves
// non-ASCII runes (so Chinese phrases survive). It does not strip
// punctuation around English tokens — containsAsToken handles word
// boundaries when needed.
func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteRune(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// containsAsToken returns true when `needle` appears inside `haystack`.
// For short, ASCII-only English needles (≤ 3 chars), it enforces word
// boundaries on both sides so "no" doesn't match "note". For everything
// else (longer English phrases, Chinese, mixed) it falls back to plain
// substring containment.
func containsAsToken(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	if !isShortASCII(needle) {
		return strings.Contains(haystack, needle)
	}
	idx := 0
	for {
		i := strings.Index(haystack[idx:], needle)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(needle)
		leftOK := start == 0 || !isWordRune(rune(haystack[start-1]))
		rightOK := end == len(haystack) || !isWordRune(rune(haystack[end]))
		if leftOK && rightOK {
			return true
		}
		idx = start + 1
		if idx >= len(haystack) {
			return false
		}
	}
}

func isShortASCII(s string) bool {
	if len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}
