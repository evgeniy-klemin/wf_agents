package workflow

import (
	"fmt"

	"github.com/eklemin/wf-agents/internal/model"
)

// transitionRule defines a valid transition with an optional guard function.
// check returns a non-empty denial reason if the guard fails, or "" to allow.
type transitionRule struct {
	from  model.Phase
	to    model.Phase
	check func(s *sessionState, evidence map[string]string) string // nil = no guard
}

// transitions is the unified table of all valid state machine transitions.
// BLOCKED is handled specially by validateTransition (not listed here).
var transitions = []transitionRule{
	{from: model.PhasePlanning, to: model.PhaseRespawn, check: nil},
	{from: model.PhasePlanning, to: model.PhaseBlocked, check: nil},
	{from: model.PhaseRespawn, to: model.PhaseDeveloping, check: guardNoActiveAgents},
	{from: model.PhaseRespawn, to: model.PhaseBlocked, check: nil},
	{from: model.PhaseDeveloping, to: model.PhaseReviewing, check: guardDirtyTree},
	{from: model.PhaseDeveloping, to: model.PhaseBlocked, check: nil},
	{from: model.PhaseReviewing, to: model.PhaseCommitting, check: nil},
	{from: model.PhaseReviewing, to: model.PhaseDeveloping, check: nil},
	{from: model.PhaseReviewing, to: model.PhaseBlocked, check: nil},
	{from: model.PhaseCommitting, to: model.PhaseRespawn, check: guardCleanTreeAndMaxIter},
	{from: model.PhaseCommitting, to: model.PhasePRCreation, check: guardCleanTree},
	{from: model.PhaseCommitting, to: model.PhaseBlocked, check: nil},
	{from: model.PhasePRCreation, to: model.PhaseFeedback, check: guardPRChecksPassed},
	{from: model.PhasePRCreation, to: model.PhaseBlocked, check: nil},
	{from: model.PhaseFeedback, to: model.PhaseComplete, check: guardPRApprovedOrMerged},
	{from: model.PhaseFeedback, to: model.PhaseRespawn, check: guardMaxIter},
	{from: model.PhaseFeedback, to: model.PhaseBlocked, check: nil},
}

// guardCleanTree requires evidence["working_tree_clean"] == "true".
func guardCleanTree(s *sessionState, evidence map[string]string) string {
	if evidence["working_tree_clean"] == "true" {
		return ""
	}
	return "working tree is not clean — commit or stash changes before leaving COMMITTING"
}

// guardDirtyTree requires evidence["working_tree_clean"] == "false".
func guardDirtyTree(_ *sessionState, evidence map[string]string) string {
	if evidence["working_tree_clean"] == "false" {
		return ""
	}
	return "no uncommitted changes found — there must be changes to review"
}

// guardNoActiveAgents requires len(s.activeAgents) == 0.
func guardNoActiveAgents(s *sessionState, _ map[string]string) string {
	if len(s.activeAgents) == 0 {
		return ""
	}
	return fmt.Sprintf("cannot leave RESPAWN with %d active subagent(s) — kill old agents before spawning new ones", len(s.activeAgents))
}

// guardPRChecksPassed requires evidence["pr_checks_pass"] == "true".
func guardPRChecksPassed(_ *sessionState, evidence map[string]string) string {
	if evidence["pr_checks_pass"] == "true" {
		return ""
	}
	return "PR checks have not passed — wait for CI to complete"
}

// guardPRApprovedOrMerged requires evidence["pr_approved"] == "true" OR evidence["pr_merged"] == "true".
func guardPRApprovedOrMerged(_ *sessionState, evidence map[string]string) string {
	if evidence["pr_approved"] == "true" || evidence["pr_merged"] == "true" {
		return ""
	}
	return "PR has not been approved or merged — requires explicit human review approval or merge before completing"
}

// guardMaxIter checks that s.iteration+1 <= s.maxIter.
// origin is the effective non-BLOCKED phase (caller must pass the resolved origin).
// For PLANNING origin, iteration is not incremented so the check is always allowed
// (but that path goes through guardNoActiveAgents, not this guard).
// This guard is used for transitions TO RESPAWN where we want to pre-check the limit.
func guardMaxIter(s *sessionState, _ map[string]string) string {
	// Determine origin phase (BLOCKED uses preBlockedPhase)
	origin := s.phase
	if origin == model.PhaseBlocked {
		origin = s.preBlockedPhase
	}
	// First entry from PLANNING doesn't count as an iteration
	if origin == model.PhasePlanning {
		return ""
	}
	if s.iteration+1 > s.maxIter {
		return fmt.Sprintf("max iterations (%d) exceeded — transition denied", s.maxIter)
	}
	return ""
}

// guardCleanTreeAndMaxIter combines guardCleanTree AND guardMaxIter.
func guardCleanTreeAndMaxIter(s *sessionState, evidence map[string]string) string {
	if reason := guardCleanTree(s, evidence); reason != "" {
		return reason
	}
	return guardMaxIter(s, evidence)
}

// validateTransition checks whether the transition from→to is allowed given the current
// session state and evidence. Returns "" to allow, or a non-empty denial reason.
//
// Special handling:
//   - Any non-terminal phase → BLOCKED is always allowed (skip guard).
//   - BLOCKED → preBlockedPhase is allowed (skip guard). Any other target is denied.
//   - All other transitions are looked up in the transitions table.
func validateTransition(s *sessionState, from, to model.Phase, evidence map[string]string) string {
	// Any non-terminal phase → BLOCKED is always allowed
	if to == model.PhaseBlocked {
		if from.IsTerminal() {
			return fmt.Sprintf("workflow already in terminal state %s", from)
		}
		return ""
	}

	// BLOCKED can only return to preBlockedPhase
	if from == model.PhaseBlocked {
		if s.preBlockedPhase == "" || to != s.preBlockedPhase {
			return fmt.Sprintf("BLOCKED can only return to %s (the pre-blocked phase)", s.preBlockedPhase)
		}
		return ""
	}

	// Look up transition in table
	for _, rule := range transitions {
		if rule.from == from && rule.to == to {
			if rule.check == nil {
				return ""
			}
			return rule.check(s, evidence)
		}
	}

	return fmt.Sprintf("transition %s → %s is not allowed", from, to)
}
