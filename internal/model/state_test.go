package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// allPhases is every phase in the system.
var allPhases = []Phase{
	PhasePlanning, PhaseRespawn, PhaseDeveloping, PhaseReviewing,
	PhaseCommitting, PhasePRCreation, PhaseFeedback,
	PhaseComplete, PhaseBlocked,
}

// expectedTransitions encodes the transition matrix.
// BLOCKED is special: CanTransitionTo allows any non-terminal, non-BLOCKED target
// (the workflow enforces preBlockedPhase on top of this).
var expectedTransitions = map[Phase]map[Phase]bool{
	PhasePlanning: {
		PhaseRespawn: true, PhaseBlocked: true,
	},
	PhaseRespawn: {
		PhaseDeveloping: true, PhaseBlocked: true,
	},
	PhaseDeveloping: {
		PhaseReviewing: true, PhaseBlocked: true,
	},
	PhaseReviewing: {
		PhaseCommitting: true, PhaseDeveloping: true, PhaseBlocked: true,
	},
	PhaseCommitting: {
		PhaseRespawn: true, PhasePRCreation: true, PhaseBlocked: true,
	},
	PhasePRCreation: {
		PhaseFeedback: true, PhaseBlocked: true,
	},
	PhaseFeedback: {
		PhaseComplete: true, PhaseRespawn: true, PhaseBlocked: true,
	},
	PhaseComplete: {
		// terminal — nothing allowed
	},
	PhaseBlocked: {
		// CanTransitionTo allows any non-terminal, non-BLOCKED
		PhasePlanning: true, PhaseRespawn: true, PhaseDeveloping: true,
		PhaseReviewing: true, PhaseCommitting: true,
		PhasePRCreation: true, PhaseFeedback: true,
	},
}

func TestCanTransitionTo_FullMatrix(t *testing.T) {
	for _, from := range allPhases {
		for _, to := range allPhases {
			expected := expectedTransitions[from][to]
			name := fmt.Sprintf("%s→%s", from, to)
			t.Run(name, func(t *testing.T) {
				actual := from.CanTransitionTo(to)
				assert.Equal(t, expected, actual,
					"%s: expected %v, got %v", name, expected, actual)
			})
		}
	}
}

func TestIsTerminal(t *testing.T) {
	for _, p := range allPhases {
		if p == PhaseComplete {
			assert.True(t, p.IsTerminal(), "%s should be terminal", p)
		} else {
			assert.False(t, p.IsTerminal(), "%s should not be terminal", p)
		}
	}
}

func TestValidTransitions_AllNonTerminalCanReachBlocked(t *testing.T) {
	for _, p := range allPhases {
		if p == PhaseComplete || p == PhaseBlocked {
			continue
		}
		assert.True(t, p.CanTransitionTo(PhaseBlocked),
			"%s should be able to transition to BLOCKED", p)
	}
}

func TestValidTransitions_CompleteIsUnreachableExceptFromFeedback(t *testing.T) {
	for _, p := range allPhases {
		if p == PhaseFeedback || p == PhaseBlocked {
			continue
		}
		assert.False(t, p.CanTransitionTo(PhaseComplete),
			"%s should NOT transition to COMPLETE", p)
	}
}

func TestValidTransitions_NoSelfTransitions(t *testing.T) {
	for _, p := range allPhases {
		if p == PhaseBlocked {
			// BLOCKED→BLOCKED is explicitly denied by CanTransitionTo
			assert.False(t, p.CanTransitionTo(p))
			continue
		}
		assert.False(t, p.CanTransitionTo(p),
			"%s should not self-transition", p)
	}
}
