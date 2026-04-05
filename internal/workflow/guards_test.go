package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeState is a test helper to build a minimal sessionState.
// agentTypes is a list of agent_type strings; each is added to the activeAgents map with a synthetic id.
func makeState(phase model.Phase, preBlocked model.Phase, iteration int, maxIter int, agentTypes []string) *sessionState {
	activeAgents := make(map[string]string)
	for _, at := range agentTypes {
		activeAgents[at] = "test-id-" + at
	}
	return &sessionState{
		phase:           phase,
		preBlockedPhase: preBlocked,
		iteration:       iteration,
		maxIter:         maxIter,
		activeAgents:    activeAgents,
		flow:            testFlow,
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
			state:    makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:     model.Phase("COMMITTING"),
			to:       model.Phase("PR_CREATION"),
			evidence: map[string]string{"working_tree_clean": "true", "branch_pushed": "true"},
		},
		{
			// Evidence guards are evaluated via SideEffect in handleTransition,
			// not in validateTransition. These pass topology + state checks.
			name:  "COMMITTING branch not pushed → PR_CREATION ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:  model.Phase("COMMITTING"),
			to:    model.Phase("PR_CREATION"),
			evidence: map[string]string{"working_tree_clean": "true", "branch_pushed": "false"},
		},
		{
			name:  "COMMITTING dirty tree → PR_CREATION ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:  model.Phase("COMMITTING"),
			to:    model.Phase("PR_CREATION"),
			evidence: map[string]string{"working_tree_clean": "false"},
		},
		{
			name:  "COMMITTING no evidence → PR_CREATION ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:  model.Phase("COMMITTING"),
			to:    model.Phase("PR_CREATION"),
			evidence: map[string]string{},
		},
		// COMMITTING → RESPAWN: requires clean tree + maxIter check
		{
			name:     "COMMITTING clean tree within maxIter → RESPAWN ALLOW",
			state:    makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:     model.Phase("COMMITTING"),
			to:       model.Phase("RESPAWN"),
			evidence: map[string]string{"working_tree_clean": "true"},
		},
		{
			// max_iterations is now checked in checkAllGuards (SideEffect), not validateTransition.
			name:  "COMMITTING at maxIter → RESPAWN ALLOW (state guards via SideEffect)",
			state: makeState(model.Phase("COMMITTING"), "", 5, 5, nil),
			from:  model.Phase("COMMITTING"),
			to:    model.Phase("RESPAWN"),
			evidence: map[string]string{"working_tree_clean": "true"},
		},
		{
			name:  "COMMITTING dirty tree → RESPAWN ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:  model.Phase("COMMITTING"),
			to:    model.Phase("RESPAWN"),
			evidence: map[string]string{"working_tree_clean": "false"},
		},

		// ---------------------------------------------------------------
		// DEVELOPING → REVIEWING: requires dirty working tree
		// ---------------------------------------------------------------
		{
			name:     "DEVELOPING dirty tree → REVIEWING ALLOW",
			state:    makeState(model.Phase("DEVELOPING"), "", 1, 5, nil),
			from:     model.Phase("DEVELOPING"),
			to:       model.Phase("REVIEWING"),
			evidence: map[string]string{"working_tree_clean": "false"},
		},
		{
			name:  "DEVELOPING clean tree → REVIEWING ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("DEVELOPING"), "", 1, 5, nil),
			from:  model.Phase("DEVELOPING"),
			to:    model.Phase("REVIEWING"),
			evidence: map[string]string{"working_tree_clean": "true"},
		},

		// ---------------------------------------------------------------
		// RESPAWN → DEVELOPING: requires no active agents
		// ---------------------------------------------------------------
		{
			name:     "RESPAWN no active agents → DEVELOPING ALLOW",
			state:    makeState(model.Phase("RESPAWN"), "", 1, 5, nil),
			from:     model.Phase("RESPAWN"),
			to:       model.Phase("DEVELOPING"),
			evidence: nil,
		},
		{
			// no_active_agents is now checked in checkAllGuards (SideEffect), not validateTransition.
			name:  "RESPAWN with active agents → DEVELOPING ALLOW (state guards via SideEffect)",
			state: makeState(model.Phase("RESPAWN"), "", 1, 5, []string{"agent-1"}),
			from:  model.Phase("RESPAWN"),
			to:    model.Phase("DEVELOPING"),
			evidence: nil,
		},

		// ---------------------------------------------------------------
		// PR_CREATION → FEEDBACK: mr_url_saved guard (evaluated via SideEffect in handleTransition)
		// ---------------------------------------------------------------
		{
			// mr_url_saved guard is evaluated via SideEffect — validateTransition always allows.
			name:     "PR_CREATION → FEEDBACK ALLOW (guard evaluated via SideEffect)",
			state:    makeState(model.Phase("PR_CREATION"), "", 1, 5, nil),
			from:     model.Phase("PR_CREATION"),
			to:       model.Phase("FEEDBACK"),
			evidence: map[string]string{},
			wantDeny: false,
		},

		// ---------------------------------------------------------------
		// FEEDBACK → COMPLETE: requires PR approved OR MR moved from draft to ready
		// ---------------------------------------------------------------
		{
			name:     "FEEDBACK PR approved → COMPLETE ALLOW",
			state:    makeState(model.Phase("FEEDBACK"), "", 1, 5, nil),
			from:     model.Phase("FEEDBACK"),
			to:       model.Phase("COMPLETE"),
			evidence: map[string]string{"review_approved": "true", "mr_ready": "false"},
		},
		{
			name:     "FEEDBACK MR ready → COMPLETE ALLOW",
			state:    makeState(model.Phase("FEEDBACK"), "", 1, 5, nil),
			from:     model.Phase("FEEDBACK"),
			to:       model.Phase("COMPLETE"),
			evidence: map[string]string{"review_approved": "false", "mr_ready": "true"},
		},
		{
			name:  "FEEDBACK neither approved nor ready → COMPLETE ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("FEEDBACK"), "", 1, 5, nil),
			from:  model.Phase("FEEDBACK"),
			to:    model.Phase("COMPLETE"),
			evidence: map[string]string{"review_approved": "false", "mr_ready": "false"},
		},
		{
			name:  "FEEDBACK no approval evidence → COMPLETE ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("FEEDBACK"), "", 1, 5, nil),
			from:  model.Phase("FEEDBACK"),
			to:    model.Phase("COMPLETE"),
			evidence: map[string]string{},
		},

		// ---------------------------------------------------------------
		// FEEDBACK → RESPAWN: maxIter check
		// ---------------------------------------------------------------
		{
			name:     "FEEDBACK within maxIter → RESPAWN ALLOW",
			state:    makeState(model.Phase("FEEDBACK"), "", 1, 5, nil),
			from:     model.Phase("FEEDBACK"),
			to:       model.Phase("RESPAWN"),
			evidence: nil,
		},
		{
			name:  "FEEDBACK at maxIter → RESPAWN ALLOW (state guards via SideEffect)",
			state: makeState(model.Phase("FEEDBACK"), "", 5, 5, nil),
			from:  model.Phase("FEEDBACK"),
			to:    model.Phase("RESPAWN"),
			evidence: nil,
		},

		// ---------------------------------------------------------------
		// BLOCKED transitions: any non-terminal → BLOCKED always allowed
		// ---------------------------------------------------------------
		{
			name:     "COMMITTING → BLOCKED always allowed (skip guards)",
			state:    makeState(model.Phase("COMMITTING"), "", 1, 5, nil),
			from:     model.Phase("COMMITTING"),
			to:       model.PhaseBlocked,
			evidence: nil,
		},
		{
			name:     "DEVELOPING → BLOCKED always allowed",
			state:    makeState(model.Phase("DEVELOPING"), "", 1, 5, nil),
			from:     model.Phase("DEVELOPING"),
			to:       model.PhaseBlocked,
			evidence: nil,
		},
		// BLOCKED → preBlockedPhase allowed
		{
			name:     "BLOCKED → preBlockedPhase ALLOW",
			state:    makeState(model.PhaseBlocked, model.Phase("DEVELOPING"), 1, 5, nil),
			from:     model.PhaseBlocked,
			to:       model.Phase("DEVELOPING"),
			evidence: nil,
		},
		// BLOCKED → wrong phase DENY
		{
			name:     "BLOCKED → wrong phase DENY",
			state:    makeState(model.PhaseBlocked, model.Phase("DEVELOPING"), 1, 5, nil),
			from:     model.PhaseBlocked,
			to:       model.Phase("REVIEWING"),
			evidence: nil,
			wantDeny: true,
		},
		// BLOCKED → BLOCKED DENY (must not re-enter BLOCKED)
		{
			name:     "BLOCKED → BLOCKED DENY",
			state:    makeState(model.PhaseBlocked, model.Phase("DEVELOPING"), 1, 5, nil),
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
			state:    makeState(model.Phase("PLANNING"), "", 1, 5, nil),
			from:     model.Phase("PLANNING"),
			to:       model.Phase("DEVELOPING"),
			evidence: nil,
			wantDeny: true,
		},

		// ---------------------------------------------------------------
		// PLANNING → RESPAWN: clean working tree required
		// ---------------------------------------------------------------
		{
			name:     "PLANNING clean tree → RESPAWN ALLOW",
			state:    makeState(model.Phase("PLANNING"), "", 1, 5, nil),
			from:     model.Phase("PLANNING"),
			to:       model.Phase("RESPAWN"),
			evidence: map[string]string{"working_tree_clean": "true"},
		},
		{
			name:  "PLANNING dirty tree → RESPAWN ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("PLANNING"), "", 1, 5, nil),
			from:  model.Phase("PLANNING"),
			to:    model.Phase("RESPAWN"),
			evidence: map[string]string{"working_tree_clean": "false"},
		},
		{
			name:  "PLANNING no evidence → RESPAWN ALLOW (evidence via SideEffect)",
			state: makeState(model.Phase("PLANNING"), "", 1, 5, nil),
			from:  model.Phase("PLANNING"),
			to:    model.Phase("RESPAWN"),
			evidence: nil,
		},

		// ---------------------------------------------------------------
		// No guards for other transitions
		// ---------------------------------------------------------------
		{
			name:     "REVIEWING → COMMITTING no guards",
			state:    makeState(model.Phase("REVIEWING"), "", 1, 5, nil),
			from:     model.Phase("REVIEWING"),
			to:       model.Phase("COMMITTING"),
			evidence: nil,
		},
		{
			name:     "REVIEWING → DEVELOPING within maxIter (reject loop)",
			state:    makeState(model.Phase("REVIEWING"), "", 1, 5, nil),
			from:     model.Phase("REVIEWING"),
			to:       model.Phase("DEVELOPING"),
			evidence: nil,
		},
		{
			name:  "REVIEWING → DEVELOPING at maxIter ALLOW (state guards via SideEffect)",
			state: makeState(model.Phase("REVIEWING"), "", 5, 5, nil),
			from:  model.Phase("REVIEWING"),
			to:    model.Phase("DEVELOPING"),
			evidence: nil,
		},
		{
			name:     "REVIEWING → RESPAWN not allowed (reject loop now goes to DEVELOPING)",
			state:    makeState(model.Phase("REVIEWING"), "", 1, 5, nil),
			from:     model.Phase("REVIEWING"),
			to:       model.Phase("RESPAWN"),
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

func TestAllowedTransitionsFor(t *testing.T) {
	t.Run("uses flow snapshot", func(t *testing.T) {
		s := makeState(model.Phase("PR_CREATION"), "", 1, 5, nil)
		got := allowedTransitionsFor(s, model.Phase("PR_CREATION"))
		assert.Contains(t, got, "FEEDBACK", "PR_CREATION should allow FEEDBACK")
	})

	t.Run("uses flow snapshot when present", func(t *testing.T) {
		s := makeState(model.Phase("DEVELOPING"), "", 1, 5, nil)
		s.flow = &model.FlowSnapshot{
			Transitions: map[string][]model.FlowTransition{
				"DEVELOPING": {
					{To: "REVIEWING"},
					{To: "BLOCKED"},
				},
			},
		}
		got := allowedTransitionsFor(s, model.Phase("DEVELOPING"))
		assert.ElementsMatch(t, []string{"REVIEWING", "BLOCKED"}, got)
	})

	t.Run("returns nil for unknown phase", func(t *testing.T) {
		s := makeState(model.Phase("COMPLETE"), "", 1, 5, nil)
		got := allowedTransitionsFor(s, model.Phase("COMPLETE"))
		assert.Empty(t, got)
	})
}

func TestFlowSnapshotAllowedTransitions(t *testing.T) {
	f := &model.FlowSnapshot{
		Transitions: map[string][]model.FlowTransition{
			"PR_CREATION": {{To: "FEEDBACK"}, {To: "BLOCKED"}},
		},
	}
	assert.ElementsMatch(t, []string{"FEEDBACK", "BLOCKED"}, f.AllowedTransitions("PR_CREATION"))
	assert.Empty(t, f.AllowedTransitions("COMPLETE"))

	var nilFlow *model.FlowSnapshot
	assert.Nil(t, nilFlow.AllowedTransitions("PR_CREATION"))
}

func TestIsAllowedGitInPlanning_Pull(t *testing.T) {
	assert.True(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git pull"), "git pull should be allowed in PLANNING")
}

func TestIsAllowedGitInPlanning_Fetch(t *testing.T) {
	assert.True(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git fetch origin main"), "git fetch with args should be allowed in PLANNING")
}

// TestTeammatePermissions verifies config-driven per-phase/per-agent tool restrictions.
func TestTeammatePermissions(t *testing.T) {
	activeAgents := []string{"developer-1"}

	makeInput := func(cmd string) []byte {
		if cmd == "" {
			return []byte(`{}`)
		}
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}
	makeFileInput := func(path string) []byte {
		b, _ := json.Marshal(map[string]string{"file_path": path})
		return b
	}

	// developer-1 Edit allowed in DEVELOPING
	t.Run("developer Edit allowed in DEVELOPING", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.False(t, result.Denied)
	})

	// developer-1 Edit denied in COMMITTING (file writes not allowed; only git operations)
	t.Run("developer Edit allowed in COMMITTING", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("COMMITTING"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied)
		assert.Contains(t, result.Reason, "COMMITTING")
	})

	// developer-1 Edit denied in REVIEWING
	t.Run("developer Edit denied in REVIEWING", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied)
		assert.Contains(t, result.Reason, "REVIEWING")
	})

	// developer-1 Edit denied in FEEDBACK
	t.Run("developer Edit denied in FEEDBACK", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("FEEDBACK"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied)
		assert.Contains(t, result.Reason, "FEEDBACK")
	})

	// developer-1 Edit denied in PR_CREATION
	t.Run("developer Edit denied in PR_CREATION", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("PR_CREATION"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied)
		assert.Contains(t, result.Reason, "PR_CREATION")
	})

	// Claude infra files (plan/memory) bypass rules
	t.Run("developer Edit plan file bypasses permission rule", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/home/user/.claude/plans/plan.md"), "developer-1", activeAgents)
		assert.False(t, result.Denied)
	})

	t.Run("developer Edit memory file bypasses permission rule", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/home/user/.claude/projects/myproject/memory/notes.md"), "developer-1", activeAgents)
		assert.False(t, result.Denied)
	})

	// No matching rule (reviewer-1 with Edit) → tool allowed (default open)
	t.Run("reviewer Edit has no matching rule - allowed", func(t *testing.T) {
		reviewerAgents := []string{"reviewer-1"}
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/project/main.go"), "reviewer-1", reviewerAgents)
		assert.False(t, result.Denied)
	})

	// developer-1 Write denied in REVIEWING
	t.Run("developer Write denied in REVIEWING", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("REVIEWING"), "Write", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied)
	})

	// developer-1 NotebookEdit denied in FEEDBACK
	t.Run("developer NotebookEdit denied in FEEDBACK", func(t *testing.T) {
		result := CheckToolPermission(model.Phase("FEEDBACK"), "NotebookEdit", makeInput(""), "developer-1", activeAgents)
		assert.True(t, result.Denied)
	})

	// Regression: agent name vs UUID — glob "developer*" must match agent name, not UUID.
	// hook-handler must pass resolveAgentName() (e.g. "developer-1"), not raw agent_id (UUID).
	t.Run("UUID as agentID does not match developer glob - rule not found - allowed", func(t *testing.T) {
		// If UUID is passed instead of agent name, glob "developer*" won't match,
		// no rule is found, and the tool is allowed by default — this is the bug.
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/project/main.go"), "a8b02535bf2798948", activeAgents)
		assert.False(t, result.Denied, "UUID does not match glob — no rule found, default open")
	})

	t.Run("agent name developer-1 matches glob and is denied in REVIEWING", func(t *testing.T) {
		// When agent name is passed correctly, glob "developer*" matches and the rule applies.
		result := CheckToolPermission(model.Phase("REVIEWING"), "Edit", makeFileInput("/project/main.go"), "developer-1", activeAgents)
		assert.True(t, result.Denied, "agent name matches glob — rule applies, denied in REVIEWING")
		assert.Contains(t, result.Reason, "REVIEWING")
	})
}

// TestValidateTransition_SnapshotTopologyConfigGuards verifies that the flow
// snapshot governs topology (which transitions exist) while guards (when/message)
// are always read from the current config — allowing guard updates on the fly.
func TestValidateTransition_SnapshotTopologyConfigGuards(t *testing.T) {
	// Build a flow snapshot with a transition that does NOT exist in defaults.yaml.
	// The snapshot's when expression should be IGNORED — guards come from config.
	flow := &model.FlowSnapshot{
		Transitions: map[string][]model.FlowTransition{
			"PLANNING": {
				{
					To:      "RESPAWN",
					When:    `jira_task_status == "In Dev"`,
					Message: "snapshot guard — should be ignored",
				},
			},
		},
	}

	t.Run("topology from snapshot allows transition", func(t *testing.T) {
		s := makeState(model.Phase("PLANNING"), "", 1, 5, nil)
		s.flow = flow
		// PLANNING→RESPAWN exists in snapshot topology, so transition is structurally valid.
		// Guards come from config (defaults.yaml), not snapshot — defaults.yaml has
		// working_tree_clean guard for PLANNING→RESPAWN, not jira_task_status.
		reason := validateTransition(s, model.Phase("PLANNING"), model.Phase("RESPAWN"),
			map[string]string{"working_tree_clean": "true"})
		assert.Empty(t, reason, "should allow — topology from snapshot, guard from config (working_tree_clean=true)")
	})

	t.Run("snapshot guard expression is not used", func(t *testing.T) {
		s := makeState(model.Phase("PLANNING"), "", 1, 5, nil)
		s.flow = flow
		// If snapshot guards were used, missing jira_task_status would deny.
		// But guards come from config, so only working_tree_clean matters.
		reason := validateTransition(s, model.Phase("PLANNING"), model.Phase("RESPAWN"),
			map[string]string{"working_tree_clean": "true"})
		assert.Empty(t, reason, "snapshot when expression should be ignored — guards from config")
	})

	t.Run("topology from snapshot blocks invalid transition", func(t *testing.T) {
		s := makeState(model.Phase("PLANNING"), "", 1, 5, nil)
		s.flow = flow
		// PLANNING→COMPLETE is not in the snapshot topology.
		reason := validateTransition(s, model.Phase("PLANNING"), model.Phase("COMPLETE"),
			map[string]string{"working_tree_clean": "true"})
		assert.NotEmpty(t, reason, "should deny — transition not in snapshot topology")
	})
}

// TestCheckAllGuards tests the pure guard evaluation function using defaults config.
// This covers both evidence-based and state-based guards evaluated via SideEffect.
func TestCheckAllGuards(t *testing.T) {
	cfg, err := config.DefaultConfig()
	require.NoError(t, err)

	tests := []struct {
		name     string
		params   guardParams
		wantDeny bool
	}{
		// Evidence guards: COMMITTING → PR_CREATION
		{
			name:   "COMMITTING clean+pushed → PR_CREATION ALLOW",
			params: guardParams{From: "COMMITTING", To: "PR_CREATION", Evidence: map[string]string{"working_tree_clean": "true", "branch_pushed": "true"}, MaxIterations: 5},
		},
		{
			name:     "COMMITTING branch not pushed → PR_CREATION DENY",
			params:   guardParams{From: "COMMITTING", To: "PR_CREATION", Evidence: map[string]string{"working_tree_clean": "true", "branch_pushed": "false"}, MaxIterations: 5},
			wantDeny: true,
		},
		{
			name:     "COMMITTING dirty tree → PR_CREATION DENY",
			params:   guardParams{From: "COMMITTING", To: "PR_CREATION", Evidence: map[string]string{"working_tree_clean": "false"}, MaxIterations: 5},
			wantDeny: true,
		},
		// Evidence guards: COMMITTING → RESPAWN
		{
			name:   "COMMITTING clean → RESPAWN ALLOW",
			params: guardParams{From: "COMMITTING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "true"}, Iteration: 1, MaxIterations: 5},
		},
		{
			name:     "COMMITTING dirty → RESPAWN DENY",
			params:   guardParams{From: "COMMITTING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "false"}, Iteration: 1, MaxIterations: 5},
			wantDeny: true,
		},
		// Evidence guards: DEVELOPING → REVIEWING
		{
			name:   "DEVELOPING dirty tree → REVIEWING ALLOW",
			params: guardParams{From: "DEVELOPING", To: "REVIEWING", Evidence: map[string]string{"working_tree_clean": "false"}, MaxIterations: 5},
		},
		{
			name:     "DEVELOPING clean tree → REVIEWING DENY",
			params:   guardParams{From: "DEVELOPING", To: "REVIEWING", Evidence: map[string]string{"working_tree_clean": "true"}, MaxIterations: 5},
			wantDeny: true,
		},
		// Evidence guards: FEEDBACK → COMPLETE
		{
			name:   "FEEDBACK approved → COMPLETE ALLOW",
			params: guardParams{From: "FEEDBACK", To: "COMPLETE", Evidence: map[string]string{"review_approved": "true"}, MaxIterations: 5},
		},
		{
			name:     "FEEDBACK neither → COMPLETE DENY",
			params:   guardParams{From: "FEEDBACK", To: "COMPLETE", Evidence: map[string]string{"review_approved": "false", "mr_ready": "false"}, MaxIterations: 5},
			wantDeny: true,
		},
		// Evidence guards: PLANNING → RESPAWN
		{
			name:   "PLANNING clean → RESPAWN ALLOW",
			params: guardParams{From: "PLANNING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "true"}, OriginPhase: "PLANNING", MaxIterations: 5},
		},
		{
			name:     "PLANNING dirty → RESPAWN DENY",
			params:   guardParams{From: "PLANNING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "false"}, OriginPhase: "PLANNING", MaxIterations: 5},
			wantDeny: true,
		},
		// State guard: max_iterations
		{
			name:     "COMMITTING at maxIter → RESPAWN DENY",
			params:   guardParams{From: "COMMITTING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "true"}, Iteration: 5, MaxIterations: 5},
			wantDeny: true,
		},
		{
			name:   "PLANNING at maxIter → RESPAWN ALLOW (exempt)",
			params: guardParams{From: "PLANNING", To: "RESPAWN", Evidence: map[string]string{"working_tree_clean": "true"}, Iteration: 5, MaxIterations: 5, OriginPhase: "PLANNING"},
		},
		// State guard: no_active_agents
		{
			name:   "RESPAWN no agents → DEVELOPING ALLOW",
			params: guardParams{From: "RESPAWN", To: "DEVELOPING", ActiveAgents: 0, MaxIterations: 5},
		},
		{
			name:     "RESPAWN with agents → DEVELOPING DENY",
			params:   guardParams{From: "RESPAWN", To: "DEVELOPING", ActiveAgents: 1, MaxIterations: 5},
			wantDeny: true,
		},
		// State guard: mr_url_saved
		{
			name:   "PR_CREATION with MrUrl → FEEDBACK ALLOW",
			params: guardParams{From: "PR_CREATION", To: "FEEDBACK", MrUrl: "https://example.com/mr/1"},
		},
		{
			name:     "PR_CREATION without MrUrl → FEEDBACK DENY",
			params:   guardParams{From: "PR_CREATION", To: "FEEDBACK", MrUrl: ""},
			wantDeny: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason := checkAllGuards(cfg, tc.params)
			if tc.wantDeny {
				assert.NotEmpty(t, reason, "expected guard denial")
			} else {
				assert.Empty(t, reason, "expected guard to pass, got: %s", reason)
			}
		})
	}
}

// TestTeammateBashAutoApprove verifies that teammates get Bash commands auto-approved
// when the command is not denied, even if the command is not in the auto-approve list.
func TestTeammateBashAutoApprove(t *testing.T) {
	activeAgents := []string{"developer-agent-1"}
	teammateID := "developer-agent-1"
	teamLeadID := "" // empty = Team Lead

	makeInput := func(cmd string) []byte {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}

	// Test 1: teammate Bash command NOT in auto-approve list (make build) gets Allowed: true
	t.Run("teammate non-auto-approve bash gets auto-approved", func(t *testing.T) {
		result := CheckToolPermission(
			model.Phase("DEVELOPING"),
			"Bash",
			makeInput("make build"),
			teammateID,
			activeAgents,
		)
		assert.False(t, result.Denied, "make build should not be denied for teammate")
		assert.True(t, result.Allowed, "make build should be auto-approved (Allowed=true) for teammate")
	})

	// Test 2: Team Lead Bash command NOT in auto-approve list does NOT get auto-approved
	t.Run("team lead non-auto-approve bash not auto-approved", func(t *testing.T) {
		result := CheckToolPermission(
			model.Phase("DEVELOPING"),
			"Bash",
			makeInput("make build"),
			teamLeadID,
			activeAgents,
		)
		assert.False(t, result.Denied, "make build should not be denied for Team Lead")
		assert.False(t, result.Allowed, "make build should NOT be auto-approved for Team Lead")
	})

	// Test 3: denied Bash command (git commit) is still denied for teammates
	t.Run("denied bash command still denied for teammate", func(t *testing.T) {
		result := CheckToolPermission(
			model.Phase("DEVELOPING"),
			"Bash",
			makeInput("git commit -m 'test'"),
			teammateID,
			activeAgents,
		)
		assert.True(t, result.Denied, "git commit should be denied for teammate in DEVELOPING phase")
	})
}

// TestGuardConfig_DefaultsIgnoreProjectOverrides proves the bug: the global guardConfig
// (initialized from defaults only via init()) does NOT contain project-level overrides.
// This test PASSES (it documents the existing broken behavior).
func TestGuardConfig_DefaultsIgnoreProjectOverrides(t *testing.T) {
	// Write a project workflow.yaml with a custom safe_command not in defaults.
	tmpDir := t.TempDir()
	wfAgentsDir := filepath.Join(tmpDir, ".wf-agents")
	if err := os.MkdirAll(wfAgentsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `phases:
  defaults:
    permissions:
      safe_commands:
        - my-custom-project-command
`
	if err := os.WriteFile(filepath.Join(wfAgentsDir, "workflow.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}

	// The global guardConfig was initialized from defaults only (init()).
	// It must NOT contain the project override — this proves the bug.
	safeCommands := guardConfig.SafeCommands()
	found := false
	for _, cmd := range safeCommands {
		if cmd == "my-custom-project-command" {
			found = true
			break
		}
	}
	assert.False(t, found, "bug confirmed: global guardConfig does not load project overrides; my-custom-project-command should NOT be present")
}

// TestInitGuardConfig_LoadsProjectOverrides verifies the fix: after calling
// InitGuardConfig(projectDir), the global guardConfig contains project-level overrides.
func TestInitGuardConfig_LoadsProjectOverrides(t *testing.T) {
	// Write a project workflow.yaml with a custom safe_command not in defaults.
	tmpDir := t.TempDir()
	wfAgentsDir := filepath.Join(tmpDir, ".wf-agents")
	if err := os.MkdirAll(wfAgentsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `phases:
  defaults:
    permissions:
      safe_commands:
        - my-custom-project-command
`
	if err := os.WriteFile(filepath.Join(wfAgentsDir, "workflow.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}

	if err := InitGuardConfig(tmpDir); err != nil {
		t.Fatalf("InitGuardConfig: %v", err)
	}
	t.Cleanup(func() {
		// Restore defaults so other tests are unaffected.
		_ = InitGuardConfig(t.TempDir())
	})

	safeCommands := guardConfig.SafeCommands()
	found := false
	for _, cmd := range safeCommands {
		if cmd == "my-custom-project-command" {
			found = true
			break
		}
	}
	assert.True(t, found, "InitGuardConfig should load project overrides; my-custom-project-command must be present")
}
