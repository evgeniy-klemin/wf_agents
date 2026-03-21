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

// terminalPhases is a config-driven set of terminal phases.
// When nil, falls back to the hardcoded default (PhaseComplete only).
var terminalPhases map[Phase]bool

// SetTerminalPhases overrides the terminal phase set from config.
// BLOCKED is never terminal regardless of config.
func SetTerminalPhases(phases []string) {
	terminalPhases = make(map[Phase]bool)
	for _, p := range phases {
		terminalPhases[Phase(p)] = true
	}
}

// IsTerminal returns true if the phase is a terminal state.
// BLOCKED is never terminal — it is a pause, not a terminal state.
func (p Phase) IsTerminal() bool {
	if p == PhaseBlocked {
		return false
	}
	if terminalPhases != nil {
		return terminalPhases[p]
	}
	return p == PhaseComplete
}
