package model

// Phase represents a workflow phase in the coding session state machine.
type Phase string

const (
	PhasePlanning   Phase = "PLANNING"
	PhaseRespawn    Phase = "RESPAWN"
	PhaseDeveloping Phase = "DEVELOPING"
	PhaseReviewing  Phase = "REVIEWING"
	PhaseCommitting Phase = "COMMITTING"
	PhasePRCreation Phase = "PR_CREATION"
	PhaseFeedback   Phase = "FEEDBACK"
	PhaseComplete   Phase = "COMPLETE"
	PhaseBlocked    Phase = "BLOCKED"
)

// ValidTransitions defines allowed state transitions.
// BLOCKED is reachable from every non-terminal state (handled separately).
var ValidTransitions = map[Phase][]Phase{
	PhasePlanning:   {PhaseRespawn, PhaseBlocked},
	PhaseRespawn:    {PhaseDeveloping, PhaseBlocked},
	PhaseDeveloping: {PhaseReviewing, PhaseBlocked},
	PhaseReviewing:  {PhaseCommitting, PhaseDeveloping, PhaseBlocked}, // reject → back to dev
	PhaseCommitting: {PhaseRespawn, PhasePRCreation, PhaseBlocked},     // respawn = more iterations, pr_creation = all done
	PhasePRCreation: {PhaseFeedback, PhaseBlocked},
	PhaseFeedback:   {PhaseComplete, PhaseRespawn, PhaseBlocked}, // respawn = iterate on feedback
}

// IsTerminal returns true if the phase is a terminal state.
// Only COMPLETE is terminal. BLOCKED is a pause, not terminal.
func (p Phase) IsTerminal() bool {
	return p == PhaseComplete
}

// CanTransitionTo checks if transitioning from this phase to target is valid.
// BLOCKED can return to any phase (validated by preBlockedPhase in the workflow).
func (p Phase) CanTransitionTo(target Phase) bool {
	// BLOCKED can transition back to any non-terminal phase (guard enforced by workflow).
	if p == PhaseBlocked {
		return !target.IsTerminal() && target != PhaseBlocked
	}
	allowed, ok := ValidTransitions[p]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}
