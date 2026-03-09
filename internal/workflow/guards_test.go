package workflow

import (
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestCheckGuards(t *testing.T) {
	tests := []struct {
		name     string
		from     model.Phase
		to       model.Phase
		evidence map[string]string
		wantDeny bool
	}{
		// COMMITTING → any: requires clean working tree
		{
			name:     "COMMITTING clean tree → ALLOW",
			from:     model.PhaseCommitting,
			to:       model.PhasePRCreation,
			evidence: map[string]string{"working_tree_clean": "true", "pr_checks_pass": "true"},
		},
		{
			name:     "COMMITTING dirty tree → DENY",
			from:     model.PhaseCommitting,
			to:       model.PhasePRCreation,
			evidence: map[string]string{"working_tree_clean": "false", "pr_checks_pass": "true"},
			wantDeny: true,
		},
		{
			name:     "COMMITTING no evidence → DENY",
			from:     model.PhaseCommitting,
			to:       model.PhaseRespawn,
			evidence: map[string]string{},
			wantDeny: true,
		},

		// DEVELOPING → REVIEWING: requires dirty working tree
		{
			name:     "DEVELOPING dirty tree → REVIEWING ALLOW",
			from:     model.PhaseDeveloping,
			to:       model.PhaseReviewing,
			evidence: map[string]string{"working_tree_clean": "false"},
		},
		{
			name:     "DEVELOPING clean tree → REVIEWING DENY",
			from:     model.PhaseDeveloping,
			to:       model.PhaseReviewing,
			evidence: map[string]string{"working_tree_clean": "true"},
			wantDeny: true,
		},

		// PR_CREATION → FEEDBACK: requires PR checks pass
		{
			name:     "PR_CREATION checks pass → FEEDBACK ALLOW",
			from:     model.PhasePRCreation,
			to:       model.PhaseFeedback,
			evidence: map[string]string{"pr_checks_pass": "true"},
		},
		{
			name:     "PR_CREATION checks fail → FEEDBACK DENY",
			from:     model.PhasePRCreation,
			to:       model.PhaseFeedback,
			evidence: map[string]string{"pr_checks_pass": "false"},
			wantDeny: true,
		},

		// FEEDBACK → COMPLETE: requires PR approved OR PR merged
		{
			name:     "FEEDBACK PR approved → COMPLETE ALLOW",
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "true", "pr_merged": "false"},
		},
		{
			name:     "FEEDBACK PR merged → COMPLETE ALLOW",
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "false", "pr_merged": "true"},
		},
		{
			name:     "FEEDBACK PR not approved and not merged → COMPLETE DENY",
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{"pr_approved": "false", "pr_merged": "false"},
			wantDeny: true,
		},
		{
			name:     "FEEDBACK no approval evidence → COMPLETE DENY",
			from:     model.PhaseFeedback,
			to:       model.PhaseComplete,
			evidence: map[string]string{},
			wantDeny: true,
		},

		// BLOCKED transitions skip guards
		{
			name:     "any → BLOCKED skips guards",
			from:     model.PhaseCommitting,
			to:       model.PhaseBlocked,
			evidence: nil,
		},
		{
			name:     "BLOCKED → any skips guards",
			from:     model.PhaseBlocked,
			to:       model.PhaseDeveloping,
			evidence: nil,
		},

		// No guards for other transitions
		{
			name:     "PLANNING → RESPAWN no guards",
			from:     model.PhasePlanning,
			to:       model.PhaseRespawn,
			evidence: nil,
		},
		{
			name:     "REVIEWING → COMMITTING no guards",
			from:     model.PhaseReviewing,
			to:       model.PhaseCommitting,
			evidence: nil,
		},
		{
			name:     "FEEDBACK → RESPAWN no guards on pr_checks",
			from:     model.PhaseFeedback,
			to:       model.PhaseRespawn,
			evidence: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := checkGuards(tt.from, tt.to, tt.evidence)
			if tt.wantDeny {
				assert.NotEmpty(t, reason, "expected guard denial")
			} else {
				assert.Empty(t, reason, "expected guard to pass, got: %s", reason)
			}
		})
	}
}
