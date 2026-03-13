package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// allPhases is every phase in the system.
var allPhases = []Phase{
	PhasePlanning, PhaseRespawn, PhaseDeveloping, PhaseReviewing,
	PhaseCommitting, PhasePRCreation, PhaseFeedback,
	PhaseComplete, PhaseBlocked,
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
