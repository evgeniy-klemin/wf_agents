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

// IsTerminal returns true if the phase is a terminal state.
// Only COMPLETE is terminal. BLOCKED is a pause, not terminal.
func (p Phase) IsTerminal() bool {
	return p == PhaseComplete
}
