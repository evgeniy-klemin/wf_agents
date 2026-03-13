package workflow

import (
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

// makeState is a test helper to build a minimal sessionState.
func makeState(phase model.Phase, preBlocked model.Phase, iteration int, maxIter int, activeAgents []string) *sessionState {
	agents := activeAgents
	if agents == nil {
		agents = []string{}
	}
	return &sessionState{
		phase:           phase,
		preBlockedPhase: preBlocked,
		iteration:       iteration,
		maxIter:         maxIter,
		activeAgents:    agents,
	}
}

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name     string
		state    *sessionState
		from     model.Phase
		to       model.Phase
		evidence map[string]string
		wantDeny bool
	}{
		// ---------------------------------------------------------------
		// COMMITTING → any: requires clean working tree
		// ---------------------------------------------------------------
		{
			name:     "COMMITTING clean tree → PR_CREATION ALLOW",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhasePRCreation,
			evidence: map[string]string{"working_tree_clean": "true"},
		},
		{
			name:     "COMMITTING dirty tree → PR_CREATION DENY",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhasePRCreation,
			evidence: map[string]string{"working_tree_clean": "false"},
			wantDeny: true,
		},
		{
			name:     "COMMITTING no evidence → PR_CREATION DENY",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhasePRCreation,
			evidence: map[string]string{},
			wantDeny: true,
		},
		// COMMITTING → RESPAWN: requires clean tree + maxIter check
		{
			name:     "COMMITTING clean tree within maxIter → RESPAWN ALLOW",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhaseRespawn,
			evidence: map[string]string{"working_tree_clean": "true"},
		},
		{
			name:     "COMMITTING clean tree at maxIter → RESPAWN DENY (would exceed)",
			state:    makeState(model.PhaseCommitting, "", 5, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhaseRespawn,
			evidence: map[string]string{"working_tree_clean": "true"},
			wantDeny: true,
		},
		{
			name:     "COMMITTING dirty tree → RESPAWN DENY",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhaseRespawn,
			evidence: map[string]string{"working_tree_clean": "false"},
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// DEVELOPING → REVIEWING: requires dirty working tree
		// ---------------------------------------------------------------
		{
			name:     "DEVELOPING dirty tree → REVIEWING ALLOW",
			state:    makeState(model.PhaseDeveloping, "", 1, 5, nil),
			from:     model.PhaseDeveloping,
			to:       model.PhaseReviewing,
			evidence: map[string]string{"working_tree_clean": "false"},
		},
		{
			name:     "DEVELOPING clean tree → REVIEWING DENY",
			state:    makeState(model.PhaseDeveloping, "", 1, 5, nil),
			from:     model.PhaseDeveloping,
			to:       model.PhaseReviewing,
			evidence: map[string]string{"working_tree_clean": "true"},
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// RESPAWN → DEVELOPING: requires no active agents
		// ---------------------------------------------------------------
		{
			name:     "RESPAWN no active agents → DEVELOPING ALLOW",
			state:    makeState(model.PhaseRespawn, "", 1, 5, nil),
			from:     model.PhaseRespawn,
			to:       model.PhaseDeveloping,
			evidence: nil,
		},
		{
			name:     "RESPAWN with active agents → DEVELOPING DENY",
			state:    makeState(model.PhaseRespawn, "", 1, 5, []string{"agent-1"}),
			from:     model.PhaseRespawn,
			to:       model.PhaseDeveloping,
			evidence: nil,
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// PR_CREATION → FEEDBACK: requires PR checks pass
		// ---------------------------------------------------------------
		{
			name:     "PR_CREATION checks pass → FEEDBACK ALLOW",
			state:    makeState(model.PhasePRCreation, "", 1, 5, nil),
			from:     model.PhasePRCreation,
			to:       model.PhaseFeedback,
			evidence: map[string]string{"pr_checks_pass": "true"},
		},
		{
			name:     "PR_CREATION checks fail → FEEDBACK DENY",
			state:    makeState(model.PhasePRCreation, "", 1, 5, nil),
			from:     model.PhasePRCreation,
			to:       model.PhaseFeedback,
			evidence: map[string]string{"pr_checks_pass": "false"},
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// FEEDBACK → COMPLETE: requires PR approved OR PR merged
		// ---------------------------------------------------------------
		{
			name:     "FEEDBACK PR approved → COMPLETE ALLOW",
			state:    makeState(model.PhaseFeedback, "", 1, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "true", "pr_merged": "false"},
		},
		{
			name:     "FEEDBACK PR merged → COMPLETE ALLOW",
			state:    makeState(model.PhaseFeedback, "", 1, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "false", "pr_merged": "true"},
		},
		{
			name:     "FEEDBACK neither approved nor merged → COMPLETE DENY",
			state:    makeState(model.PhaseFeedback, "", 1, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "false", "pr_merged": "false"},
			wantDeny: true,
		},
		{
			name:     "FEEDBACK no approval evidence → COMPLETE DENY",
			state:    makeState(model.PhaseFeedback, "", 1, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{},
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// FEEDBACK → RESPAWN: maxIter check
		// ---------------------------------------------------------------
		{
			name:     "FEEDBACK within maxIter → RESPAWN ALLOW",
			state:    makeState(model.PhaseFeedback, "", 1, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseRespawn,
			evidence: nil,
		},
		{
			name:     "FEEDBACK at maxIter → RESPAWN DENY",
			state:    makeState(model.PhaseFeedback, "", 5, 5, nil),
			from:     model.PhaseFeedback,
			to:       model.PhaseRespawn,
			evidence: nil,
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// BLOCKED transitions: any non-terminal → BLOCKED always allowed
		// ---------------------------------------------------------------
		{
			name:     "COMMITTING → BLOCKED always allowed (skip guards)",
			state:    makeState(model.PhaseCommitting, "", 1, 5, nil),
			from:     model.PhaseCommitting,
			to:       model.PhaseBlocked,
			evidence: nil,
		},
		{
			name:     "DEVELOPING → BLOCKED always allowed",
			state:    makeState(model.PhaseDeveloping, "", 1, 5, nil),
			from:     model.PhaseDeveloping,
			to:       model.PhaseBlocked,
			evidence: nil,
		},
		// BLOCKED → preBlockedPhase allowed
		{
			name:     "BLOCKED → preBlockedPhase ALLOW",
			state:    makeState(model.PhaseBlocked, model.PhaseDeveloping, 1, 5, nil),
			from:     model.PhaseBlocked,
			to:       model.PhaseDeveloping,
			evidence: nil,
		},
		// BLOCKED → wrong phase DENY
		{
			name:     "BLOCKED → wrong phase DENY",
			state:    makeState(model.PhaseBlocked, model.PhaseDeveloping, 1, 5, nil),
			from:     model.PhaseBlocked,
			to:       model.PhaseReviewing,
			evidence: nil,
			wantDeny: true,
		},
		// BLOCKED → BLOCKED DENY (must not re-enter BLOCKED)
		{
			name:     "BLOCKED → BLOCKED DENY",
			state:    makeState(model.PhaseBlocked, model.PhaseDeveloping, 1, 5, nil),
			from:     model.PhaseBlocked,
			to:       model.PhaseBlocked,
			evidence: nil,
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// Invalid transitions not in the table
		// ---------------------------------------------------------------
		{
			name:     "PLANNING → DEVELOPING not allowed",
			state:    makeState(model.PhasePlanning, "", 1, 5, nil),
			from:     model.PhasePlanning,
			to:       model.PhaseDeveloping,
			evidence: nil,
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// No guards for other transitions
		// ---------------------------------------------------------------
		{
			name:     "PLANNING → RESPAWN no guards",
			state:    makeState(model.PhasePlanning, "", 1, 5, nil),
			from:     model.PhasePlanning,
			to:       model.PhaseRespawn,
			evidence: nil,
		},
		{
			name:     "REVIEWING → COMMITTING no guards",
			state:    makeState(model.PhaseReviewing, "", 1, 5, nil),
			from:     model.PhaseReviewing,
			to:       model.PhaseCommitting,
			evidence: nil,
		},
		{
			name:     "REVIEWING → RESPAWN within maxIter (reject loop)",
			state:    makeState(model.PhaseReviewing, "", 1, 5, nil),
			from:     model.PhaseReviewing,
			to:       model.PhaseRespawn,
			evidence: nil,
		},
		{
			name:     "REVIEWING → RESPAWN at maxIter DENY",
			state:    makeState(model.PhaseReviewing, "", 5, 5, nil),
			from:     model.PhaseReviewing,
			to:       model.PhaseRespawn,
			evidence: nil,
			wantDeny: true,
		},
		{
			name:     "REVIEWING → DEVELOPING not allowed (removed reject loop path)",
			state:    makeState(model.PhaseReviewing, "", 1, 5, nil),
			from:     model.PhaseReviewing,
			to:       model.PhaseDeveloping,
			evidence: nil,
			wantDeny: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := validateTransition(tt.state, tt.from, tt.to, tt.evidence)
			if tt.wantDeny {
				assert.NotEmpty(t, reason, "expected guard denial")
			} else {
				assert.Empty(t, reason, "expected transition to be allowed, got: %s", reason)
			}
		})
	}
}
