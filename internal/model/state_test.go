package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// allPhases is every phase in the system.
var allPhases = []Phase{
	Phase("PLANNING"), Phase("RESPAWN"), Phase("DEVELOPING"), Phase("REVIEWING"),
	Phase("COMMITTING"), Phase("PR_CREATION"), Phase("FEEDBACK"),
	Phase("COMPLETE"), PhaseBlocked,
}

func TestIsTerminal_WithConfig(t *testing.T) {
	SetTerminalPhases([]string{"COMPLETE"})
	t.Cleanup(func() { terminalPhases = nil })

	for _, p := range allPhases {
		if p == Phase("COMPLETE") {
			assert.True(t, p.IsTerminal(), "%s should be terminal", p)
		} else {
			assert.False(t, p.IsTerminal(), "%s should not be terminal", p)
		}
	}
}

func TestIsTerminal_NoConfig(t *testing.T) {
	terminalPhases = nil
	for _, p := range allPhases {
		assert.False(t, p.IsTerminal(), "%s should not be terminal when no config is set", p)
	}
}
