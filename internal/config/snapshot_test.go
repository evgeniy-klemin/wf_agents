package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePhaseOrder_BasicBFS(t *testing.T) {
	phases := map[string]PhaseConfig{
		"PLANNING":    {},
		"DEVELOPING":  {},
		"REVIEWING":   {},
		"COMPLETE":    {},
	}
	transitions := map[string][]TransitionConfig{
		"PLANNING":   {{To: "DEVELOPING"}},
		"DEVELOPING": {{To: "REVIEWING"}},
		"REVIEWING":  {{To: "COMPLETE"}},
	}

	order := computePhaseOrder("PLANNING", phases, transitions)
	assert.Equal(t, []string{"PLANNING", "DEVELOPING", "REVIEWING", "COMPLETE"}, order)
}

func TestComputePhaseOrder_OrphanPhasesAppendedSorted(t *testing.T) {
	phases := map[string]PhaseConfig{
		"PLANNING":   {},
		"DEVELOPING": {},
		"ORPHAN_B":   {},
		"ORPHAN_A":   {},
	}
	transitions := map[string][]TransitionConfig{
		"PLANNING": {{To: "DEVELOPING"}},
	}

	order := computePhaseOrder("PLANNING", phases, transitions)
	assert.Equal(t, []string{"PLANNING", "DEVELOPING", "ORPHAN_A", "ORPHAN_B"}, order)
}

func TestComputePhaseOrder_MultipleTransitionsFromPhase(t *testing.T) {
	phases := map[string]PhaseConfig{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}
	transitions := map[string][]TransitionConfig{
		"A": {{To: "B"}, {To: "C"}},
		"B": {{To: "D"}},
	}

	order := computePhaseOrder("A", phases, transitions)
	// BFS: A, then B and C (in transition order), then D
	assert.Equal(t, []string{"A", "B", "C", "D"}, order)
}

func TestComputePhaseOrder_NoCycles(t *testing.T) {
	phases := map[string]PhaseConfig{
		"A": {},
		"B": {},
	}
	// Cycle: A -> B -> A
	transitions := map[string][]TransitionConfig{
		"A": {{To: "B"}},
		"B": {{To: "A"}},
	}

	order := computePhaseOrder("A", phases, transitions)
	assert.Equal(t, []string{"A", "B"}, order)
}

func TestExtractFlowSnapshot_UsesYAMLOrderWhenComplete(t *testing.T) {
	cfg := &Config{
		Phases: &PhasesConfig{
			Start:      "PLANNING",
			Stop:       []string{"COMPLETE"},
			PhaseOrder: []string{"PLANNING", "DEVELOPING", "COMPLETE"},
			Phases: map[string]PhaseConfig{
				"PLANNING":   {},
				"DEVELOPING": {},
				"COMPLETE":   {},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"PLANNING":   {{To: "DEVELOPING"}},
			"DEVELOPING": {{To: "COMPLETE"}},
		},
	}

	snap := ExtractFlowSnapshot(cfg)
	assert.Equal(t, []string{"PLANNING", "DEVELOPING", "COMPLETE"}, snap.PhaseOrder)
}

func TestExtractFlowSnapshot_ComputesBFSWhenPhaseOrderNil(t *testing.T) {
	cfg := &Config{
		Phases: &PhasesConfig{
			Start:      "PLANNING",
			Stop:       []string{"COMPLETE"},
			PhaseOrder: nil, // missing
			Phases: map[string]PhaseConfig{
				"PLANNING":   {},
				"DEVELOPING": {},
				"COMPLETE":   {},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"PLANNING":   {{To: "DEVELOPING"}},
			"DEVELOPING": {{To: "COMPLETE"}},
		},
	}

	snap := ExtractFlowSnapshot(cfg)
	assert.Equal(t, []string{"PLANNING", "DEVELOPING", "COMPLETE"}, snap.PhaseOrder)
}

func TestExtractFlowSnapshot_ComputesBFSWhenPhaseOrderIncomplete(t *testing.T) {
	cfg := &Config{
		Phases: &PhasesConfig{
			Start:      "PLANNING",
			Stop:       []string{"COMPLETE"},
			PhaseOrder: []string{"PLANNING", "DEVELOPING"}, // missing COMPLETE
			Phases: map[string]PhaseConfig{
				"PLANNING":   {},
				"DEVELOPING": {},
				"COMPLETE":   {},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"PLANNING":   {{To: "DEVELOPING"}},
			"DEVELOPING": {{To: "COMPLETE"}},
		},
	}

	snap := ExtractFlowSnapshot(cfg)
	assert.Equal(t, []string{"PLANNING", "DEVELOPING", "COMPLETE"}, snap.PhaseOrder)
}
