package workflow

import (
	"fmt"

	"github.com/eklemin/wf-agents/internal/model"
)

// guardDef defines a transition precondition validated inside the Temporal workflow.
// Guards check evidence collected by the client (wf-client) and passed in SignalTransition.Guards.
// If altEvidenceKey is set, the guard passes if EITHER the primary OR alternate condition is met.
type guardDef struct {
	from           model.Phase
	to             model.Phase // empty = any target from this phase
	evidenceKey    string      // key in Guards map
	wantValue      string      // required value
	altEvidenceKey string      // optional alternative key (OR condition)
	altWantValue   string      // required value for alternative
	denyMessage    string      // message when guard fails
}

// guards is the registry of all transition guards.
// Based on NTCoding/autonomous-claude-agent-team original.
var guards = []guardDef{
	{
		from:        model.PhaseCommitting,
		to:          "", // any target
		evidenceKey: "working_tree_clean",
		wantValue:   "true",
		denyMessage: "working tree is not clean — commit or stash changes before leaving COMMITTING",
	},
	{
		from:        model.PhaseDeveloping,
		to:          model.PhaseReviewing,
		evidenceKey: "working_tree_clean",
		wantValue:   "false",
		denyMessage: "no uncommitted changes found — there must be changes to review",
	},
	{
		from:        model.PhasePRCreation,
		to:          model.PhaseFeedback,
		evidenceKey: "pr_checks_pass",
		wantValue:   "true",
		denyMessage: "PR checks have not passed — wait for CI to complete",
	},
	{
		from:           model.PhaseFeedback,
		to:             model.PhaseComplete,
		evidenceKey:    "pr_approved",
		wantValue:      "true",
		altEvidenceKey: "pr_merged",
		altWantValue:   "true",
		denyMessage:    "PR has not been approved or merged — requires explicit human review approval or merge before completing",
	},
}

// checkGuards validates transition guards against evidence provided by the client.
// Returns empty string if all guards pass, or a denial reason if any guard fails.
// Auto-transitions (BLOCKED/unblock) pass nil evidence — guards are skipped for BLOCKED transitions.
func checkGuards(from, to model.Phase, evidence map[string]string) string {
	// Skip guards for BLOCKED transitions (auto-managed by hooks)
	if to == model.PhaseBlocked || from == model.PhaseBlocked {
		return ""
	}

	for _, g := range guards {
		if g.from != from {
			continue
		}
		if g.to != "" && g.to != to {
			continue
		}

		val, ok := evidence[g.evidenceKey]
		primaryPass := ok && val == g.wantValue

		altPass := false
		if g.altEvidenceKey != "" {
			if altVal, altOk := evidence[g.altEvidenceKey]; altOk && altVal == g.altWantValue {
				altPass = true
			}
		}

		if primaryPass || altPass {
			continue
		}

		// Neither condition met
		if !ok && g.altEvidenceKey == "" {
			return fmt.Sprintf("guard %q: evidence %q not provided — collect evidence before transitioning",
				g.denyMessage, g.evidenceKey)
		}
		return g.denyMessage
	}
	return ""
}
