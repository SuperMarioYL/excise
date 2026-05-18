package session

import (
	"fmt"
	"sort"
)

// DependencyGraph models the tool_use → tool_result edge.
//
// invariant 1 of the Excise primitive — "removing a turn with tool_calls
// removes its paired tool_result turns" — is enforced here.
type DependencyGraph struct {
	// owners[tool_use_id] = id of the Turn that emitted the tool_use block.
	owners map[string]string
	// dependents[tool_use_id] = ids of Turns that contain the matching
	// tool_result block.
	dependents map[string][]string
}

// BuildGraph indexes a session's turns into a DependencyGraph.
func BuildGraph(turns []Turn) *DependencyGraph {
	g := &DependencyGraph{
		owners:     make(map[string]string),
		dependents: make(map[string][]string),
	}
	for _, t := range turns {
		for _, c := range t.ToolCalls {
			if c.ID != "" {
				g.owners[c.ID] = t.ID
			}
		}
	}
	for _, t := range turns {
		for _, r := range t.ToolResults {
			if r.ToolUseID == "" {
				continue
			}
			g.dependents[r.ToolUseID] = append(g.dependents[r.ToolUseID], t.ID)
		}
	}
	return g
}

// Closure computes the full set of turn ids that must be removed when
// `seeds` are excised, honoring invariants 1 and 2.
//
//   - invariant 1: removing a turn with tool_use blocks pulls in the matching
//     tool_result turns.
//   - invariant 2 (warn-mode, see Verify): removing a turn that contains only
//     a tool_result without its owner is allowed but flagged.
//
// The walk is iterative so it terminates even on pathological self-cycles
// (which a well-formed transcript should never contain).
func (g *DependencyGraph) Closure(turns []Turn, seeds map[string]bool) map[string]bool {
	out := map[string]bool{}
	for id := range seeds {
		out[id] = true
	}
	turnByID := make(map[string]Turn, len(turns))
	for _, t := range turns {
		turnByID[t.ID] = t
	}
	changed := true
	for changed {
		changed = false
		for id := range out {
			t, ok := turnByID[id]
			if !ok {
				continue
			}
			for _, c := range t.ToolCalls {
				if c.ID == "" {
					continue
				}
				for _, dep := range g.dependents[c.ID] {
					if !out[dep] {
						out[dep] = true
						changed = true
					}
				}
			}
		}
	}
	return out
}

// Verify returns warnings about turns in `toCut` that would leave dangling
// tool refs. invariant 2 says removing a tool_result requires its owner; we
// surface this as a warning rather than refusing so the user with --force
// can override.
type Warning struct {
	TurnID string
	Reason string
}

func (g *DependencyGraph) Verify(turns []Turn, toCut map[string]bool) []Warning {
	var warns []Warning
	turnByID := make(map[string]Turn, len(turns))
	for _, t := range turns {
		turnByID[t.ID] = t
	}
	for id := range toCut {
		t, ok := turnByID[id]
		if !ok {
			continue
		}
		// Removing a tool_result turn whose tool_use owner survives →
		// the surviving owner will reference a phantom result.
		for _, r := range t.ToolResults {
			ownerID, ok := g.owners[r.ToolUseID]
			if !ok {
				continue
			}
			if !toCut[ownerID] {
				warns = append(warns, Warning{
					TurnID: id,
					Reason: fmt.Sprintf("cutting tool_result for tool_use %s but owner turn %s would survive (invariant 2)", r.ToolUseID, ownerID),
				})
			}
		}
	}
	sort.SliceStable(warns, func(i, j int) bool { return warns[i].TurnID < warns[j].TurnID })
	return warns
}

// Excise returns a new []Turn with `toCut` (and any dependents) removed.
// The original slice is not mutated.
//
// This is the core mutation referenced by the Excise primitive in
// the MVP plan (section 2). The returned slice preserves ordering (invariant
// 3) and surviving turns keep their stable IDs.
func Excise(turns []Turn, toCut map[string]bool) []Turn {
	if len(toCut) == 0 {
		return append([]Turn(nil), turns...)
	}
	g := BuildGraph(turns)
	closure := g.Closure(turns, toCut)
	out := make([]Turn, 0, len(turns))
	for _, t := range turns {
		if closure[t.ID] {
			continue
		}
		out = append(out, t)
	}
	return out
}
