package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Parsing tests ---

func TestWorkflowDefaults_HasNonNilPhases(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.Phases, "defaults.yaml must define a phases section")
}

func TestWorkflowDefaults_StartPhase(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.Equal(t, "PLANNING", cfg.Phases.Start)
}

func TestWorkflowDefaults_StopPhases(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.Equal(t, []string{"COMPLETE"}, cfg.Phases.Stop)
}

func TestWorkflowDefaults_PhaseCount(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	// PLANNING, RESPAWN, DEVELOPING, REVIEWING, COMMITTING, PR_CREATION, FEEDBACK, COMPLETE
	assert.Len(t, cfg.Phases.Phases, 8, "expected 8 phases defined")
}

func TestWorkflowDefaults_PhaseNames(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	expected := []string{"PLANNING", "RESPAWN", "DEVELOPING", "REVIEWING", "COMMITTING", "PR_CREATION", "FEEDBACK", "COMPLETE"}
	for _, name := range expected {
		_, ok := cfg.Phases.Phases[name]
		assert.True(t, ok, "expected phase %q to be defined", name)
	}
}

func TestWorkflowDefaults_PhaseDisplay(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	planning := cfg.Phases.Phases["PLANNING"]
	assert.Equal(t, "Planning", planning.Display.Label)
	assert.Equal(t, "clipboard", planning.Display.Icon)
	assert.Equal(t, "#6366f1", planning.Display.Color)
}

func TestWorkflowDefaults_PhaseInstructions(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	// Instructions field is no longer set in defaults.yaml — filename convention is PHASE.md.
	planning := cfg.Phases.Phases["PLANNING"]
	assert.Empty(t, planning.Instructions, "instructions field should not be set in defaults.yaml")
}

func TestWorkflowDefaults_TransitionsNotNil(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg.Transitions, "defaults.yaml must define a transitions section")
}

func TestWorkflowDefaults_TransitionsCount(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	// PLANNING, RESPAWN, DEVELOPING, REVIEWING, COMMITTING, PR_CREATION, FEEDBACK = 7 source phases
	assert.GreaterOrEqual(t, len(cfg.Transitions), 7, "expected transitions for at least 7 source phases")
}

func TestWorkflowDefaults_TransitionTargets(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	cases := []struct {
		from string
		to   string
	}{
		{"PLANNING", "RESPAWN"},
		{"RESPAWN", "DEVELOPING"},
		{"DEVELOPING", "REVIEWING"},
		{"REVIEWING", "COMMITTING"},
		{"REVIEWING", "DEVELOPING"},
		{"COMMITTING", "RESPAWN"},
		{"COMMITTING", "PR_CREATION"},
		{"PR_CREATION", "FEEDBACK"},
		{"FEEDBACK", "COMPLETE"},
		{"FEEDBACK", "RESPAWN"},
	}

	for _, c := range cases {
		ts, ok := cfg.Transitions[c.from]
		require.True(t, ok, "expected transitions from %q", c.from)
		found := false
		for _, t2 := range ts {
			if t2.To == c.to {
				found = true
				break
			}
		}
		assert.True(t, found, "expected transition %s→%s", c.from, c.to)
	}
}

func TestWorkflowDefaults_DevelopingOnEnter(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	developing := cfg.Phases.Phases["DEVELOPING"]
	require.Len(t, developing.OnEnter, 1)
	assert.Equal(t, "increment_iteration", developing.OnEnter[0].Type)
}

func TestWorkflowDefaults_DefaultsPermissions(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.Phases.Defaults.Permissions.SafeCommands)
	assert.NotEmpty(t, cfg.Phases.Defaults.Permissions.SafeCommands)
}

// --- Validation tests ---

func makeValidConfig() *Config {
	return &Config{
		Phases: &PhasesConfig{
			Start: "A",
			Stop:  []string{"C"},
			Phases: map[string]PhaseConfig{
				"A": {Instructions: "a.md"},
				"B": {Instructions: "b.md"},
				"C": {Instructions: "c.md"},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"A": {{To: "B"}},
			"B": {{To: "C"}},
		},
	}
}

func TestValidateWorkflowConfig_ValidPasses(t *testing.T) {
	cfg := makeValidConfig()
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "valid config should produce no errors")
}

func TestValidateWorkflowConfig_NilPhasesSkipped(t *testing.T) {
	cfg := &Config{}
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "nil Phases should be skipped (not yet configured)")
}

func TestValidateWorkflowConfig_MissingStartPhase(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Phases.Start = "NONEXISTENT"
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "NONEXISTENT") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error mentioning missing start phase")
}

func TestValidateWorkflowConfig_MissingStopPhase(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Phases.Stop = []string{"NONEXISTENT"}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "NONEXISTENT") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error mentioning missing stop phase")
}

func TestValidateWorkflowConfig_BlockedPhaseIsError(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Phases.Phases["BLOCKED"] = PhaseConfig{}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "BLOCKED") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error about BLOCKED phase in YAML")
}

func TestValidateWorkflowConfig_DanglingTransitionTarget(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Transitions["A"] = append(cfg.Transitions["A"], TransitionConfig{To: "NONEXISTENT"})
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "NONEXISTENT") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error mentioning dangling transition target")
}

func TestValidateWorkflowConfig_StopPhaseHasNoOutgoing(t *testing.T) {
	cfg := makeValidConfig()
	// Add outgoing from stop phase
	cfg.Transitions["C"] = []TransitionConfig{{To: "A"}}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "stop") || containsStr(e.Error(), "C") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error about stop phase having outgoing transitions")
}

func TestValidateWorkflowConfig_UnreachablePhase(t *testing.T) {
	cfg := makeValidConfig()
	// Add a phase with no incoming transitions
	cfg.Phases.Phases["ORPHAN"] = PhaseConfig{}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "ORPHAN") || containsStr(e.Error(), "unreachable") || containsStr(e.Error(), "reachable") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error about unreachable phase ORPHAN")
}

func TestValidateWorkflowConfig_UnknownWhenVariable(t *testing.T) {
	// Custom bare identifiers are now valid evidence keys — no "unknown variable" error.
	// A missing message is still an error when 'when' is set.
	cfg := makeValidConfig()
	cfg.Transitions["A"] = []TransitionConfig{{To: "B", When: "custom_var", Message: "msg"}}
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "custom bare identifier with message should produce no errors")
}

func TestValidateWorkflowConfig_EmptyMessageWithWhen(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Transitions["A"] = []TransitionConfig{{To: "B", When: "working_tree_clean", Message: ""}}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "message") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error about empty message when 'when' is set")
}

func TestValidateWorkflowConfig_KnownWhenVariablesPasses(t *testing.T) {
	knownVars := []string{
		"working_tree_clean", "ci_passed", "review_approved",
		"mr_ready", "active_agents", "iteration", "max_iterations",
	}
	for _, v := range knownVars {
		cfg := makeValidConfig()
		cfg.Transitions["A"] = []TransitionConfig{{To: "B", When: v, Message: "some message"}}
		errs := ValidateWorkflowConfig(cfg)
		assert.Empty(t, errs, "known when variable %q should not produce errors", v)
	}
}

func TestValidateWorkflowConfig_InvalidAgentGlob(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Phases.Phases["A"] = PhaseConfig{
		Permissions: PhasePermissions{
			Teammate: []AgentPermission{
				{Agent: "[invalid", FileWrites: "deny"},
			},
		},
	}
	errs := ValidateWorkflowConfig(cfg)
	require.NotEmpty(t, errs)
	hasErr := false
	for _, e := range errs {
		if containsStr(e.Error(), "[invalid") || containsStr(e.Error(), "glob") || containsStr(e.Error(), "pattern") {
			hasErr = true
		}
	}
	assert.True(t, hasErr, "expected error about invalid agent glob pattern")
}

func TestValidateWorkflowConfig_DefaultConfigPasses(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "default config should pass validation")
}

// --- Helper method tests ---

func TestConfig_IsValidTransition(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	// Valid transitions
	assert.True(t, cfg.IsValidTransition("PLANNING", "RESPAWN"))
	assert.True(t, cfg.IsValidTransition("RESPAWN", "DEVELOPING"))
	assert.True(t, cfg.IsValidTransition("DEVELOPING", "REVIEWING"))
	assert.True(t, cfg.IsValidTransition("REVIEWING", "COMMITTING"))
	assert.True(t, cfg.IsValidTransition("REVIEWING", "DEVELOPING"))
	assert.True(t, cfg.IsValidTransition("COMMITTING", "RESPAWN"))
	assert.True(t, cfg.IsValidTransition("COMMITTING", "PR_CREATION"))
	assert.True(t, cfg.IsValidTransition("PR_CREATION", "FEEDBACK"))
	assert.True(t, cfg.IsValidTransition("FEEDBACK", "COMPLETE"))
	assert.True(t, cfg.IsValidTransition("FEEDBACK", "RESPAWN"))

	// Invalid transitions
	assert.False(t, cfg.IsValidTransition("PLANNING", "DEVELOPING"))
	assert.False(t, cfg.IsValidTransition("PLANNING", "COMPLETE"))
	assert.False(t, cfg.IsValidTransition("COMPLETE", "PLANNING"))
	assert.False(t, cfg.IsValidTransition("DEVELOPING", "COMMITTING"))
	assert.False(t, cfg.IsValidTransition("REVIEWING", "RESPAWN"))
}

func TestConfig_PhaseHint(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	assert.Equal(t, "Transition to RESPAWN first.", cfg.PhaseHint("PLANNING"))
	assert.Equal(t, "Only agent management allowed. Transition to DEVELOPING when agents are ready.", cfg.PhaseHint("RESPAWN"))
	assert.Equal(t, "Only git operations allowed.", cfg.PhaseHint("COMMITTING"))
	assert.Equal(t, "Only PR creation commands allowed.", cfg.PhaseHint("PR_CREATION"))

	// Unknown phase returns empty string
	assert.Equal(t, "", cfg.PhaseHint("NONEXISTENT"))
}

func TestConfig_StartPhase(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.Equal(t, "PLANNING", cfg.StartPhase())
}

func TestConfig_StopPhases(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.Equal(t, []string{"COMPLETE"}, cfg.StopPhases())
}

// --- ParseWhenExpression tests ---

func TestParseWhenExpression_Empty(t *testing.T) {
	checks := ParseWhenExpression("", "some message")
	assert.Nil(t, checks, "empty when should return nil")
}

func TestParseWhenExpression_WorkingTreeClean(t *testing.T) {
	checks := ParseWhenExpression("working_tree_clean", "tree not clean")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "working_tree_clean", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
	assert.Equal(t, "tree not clean", checks[0].Message)
}

func TestParseWhenExpression_NotWorkingTreeClean(t *testing.T) {
	checks := ParseWhenExpression("not working_tree_clean", "no changes")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "working_tree_clean", checks[0].Key)
	assert.Equal(t, "false", checks[0].Value)
}

func TestParseWhenExpression_ActiveAgents(t *testing.T) {
	checks := ParseWhenExpression("active_agents == 0", "agents still active")
	require.Len(t, checks, 1)
	assert.Equal(t, "no_active_agents", checks[0].Type)
	// EvalCheck provides its own message for no_active_agents
	assert.Empty(t, checks[0].Message)
}

func TestParseWhenExpression_Iterations(t *testing.T) {
	checks := ParseWhenExpression("iteration < max_iterations", "max reached")
	require.Len(t, checks, 1)
	assert.Equal(t, "max_iterations", checks[0].Type)
	// EvalCheck provides its own fallback message for max_iterations
	assert.Empty(t, checks[0].Message)
}

func TestParseWhenExpression_CIPassed(t *testing.T) {
	checks := ParseWhenExpression("ci_passed", "CI not passed")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "ci_passed", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
}

func TestParseWhenExpression_BranchPushed(t *testing.T) {
	checks := ParseWhenExpression("branch_pushed", "branch not pushed to remote")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "branch_pushed", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
	assert.Equal(t, "branch not pushed to remote", checks[0].Message)
}

func TestParseWhenExpression_ReviewApprovedOrMRReady(t *testing.T) {
	checks := ParseWhenExpression("review_approved or mr_ready", "not approved")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "review_approved", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
	require.Len(t, checks[0].Alternatives, 1)
	assert.Equal(t, "mr_ready", checks[0].Alternatives[0].Key)
	assert.Equal(t, "true", checks[0].Alternatives[0].Value)
}

func TestParseWhenExpression_AndExpression(t *testing.T) {
	checks := ParseWhenExpression("working_tree_clean and iteration < max_iterations", "combined message")
	require.Len(t, checks, 2)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "working_tree_clean", checks[0].Key)
	assert.Equal(t, "max_iterations", checks[1].Type)
}

func TestParseWhenExpression_BareIdentifier(t *testing.T) {
	checks := ParseWhenExpression("review_approved", "not approved")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "review_approved", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
}

func TestParseWhenExpression_BareIdentifierMRReady(t *testing.T) {
	checks := ParseWhenExpression("mr_ready", "not ready")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "mr_ready", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
}

func TestParseWhenExpression_KeyEqQuotedValue(t *testing.T) {
	checks := ParseWhenExpression(`jira_task_status == "To Merge"`, "jira status not To Merge")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "jira_task_status", checks[0].Key)
	assert.Equal(t, "To Merge", checks[0].Value)
}

func TestParseWhenExpression_MergedAndCIPassed(t *testing.T) {
	checks := ParseWhenExpression("mr_ready and ci_passed", "mr ready and CI not passed")
	require.Len(t, checks, 2)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "mr_ready", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
	assert.Equal(t, "evidence", checks[1].Type)
	assert.Equal(t, "ci_passed", checks[1].Key)
	assert.Equal(t, "true", checks[1].Value)
}

func TestParseWhenExpression_NotCIPassed(t *testing.T) {
	checks := ParseWhenExpression("not ci_passed", "CI passed")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "ci_passed", checks[0].Key)
	assert.Equal(t, "false", checks[0].Value)
}

func TestParseWhenExpression_OrExpression(t *testing.T) {
	checks := ParseWhenExpression("review_approved or mr_ready", "not approved or ready")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "review_approved", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
	require.Len(t, checks[0].Alternatives, 1)
	assert.Equal(t, "mr_ready", checks[0].Alternatives[0].Key)
	assert.Equal(t, "true", checks[0].Alternatives[0].Value)
}

func TestParseWhenExpression_CustomBareIdentifier(t *testing.T) {
	checks := ParseWhenExpression("deploy_done", "not deployed")
	require.Len(t, checks, 1)
	assert.Equal(t, "evidence", checks[0].Type)
	assert.Equal(t, "deploy_done", checks[0].Key)
	assert.Equal(t, "true", checks[0].Value)
}

func TestValidateWorkflowConfig_CustomWhenVarPasses(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Transitions["A"] = []TransitionConfig{{To: "B", When: "deploy_done", Message: "not deployed"}}
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "custom bare identifier should not produce validation errors")
}

func TestValidateWorkflowConfig_QuotedStringInWhenPasses(t *testing.T) {
	cfg := makeValidConfig()
	cfg.Transitions["A"] = []TransitionConfig{{To: "B", When: `jira_task_status == "To Merge"`, Message: "jira status wrong"}}
	errs := ValidateWorkflowConfig(cfg)
	assert.Empty(t, errs, "quoted value in when expression should not flag as unknown")
}

func TestParseWhenExpression_AllDefaultTransitions(t *testing.T) {
	// Verify all when expressions in the default config can be parsed without unknown tokens.
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.Transitions)

	for from, transitions := range cfg.Transitions {
		for _, t2 := range transitions {
			checks := ParseWhenExpression(t2.When, t2.Message)
			for _, c := range checks {
				assert.NotContains(t, c.Key, "_unknown_when_expr_",
					"unknown when expression in %s→%s: %q", from, t2.To, t2.When)
			}
		}
	}
}

func TestIsTeammate(t *testing.T) {
	cfg := &Config{
		Phases: &PhasesConfig{
			Defaults: PhaseDefaults{
				Permissions: DefaultPermissions{
					Teammate: []AgentPermission{
						{Agent: "developer*", FileWrites: "deny"},
					},
				},
			},
			Phases: map[string]PhaseConfig{
				"REVIEWING": {
					Permissions: PhasePermissions{
						Teammate: []AgentPermission{
							{Agent: "reviewer*", FileWrites: "deny"},
						},
					},
				},
			},
		},
	}

	// Matches default teammate glob
	assert.True(t, cfg.IsTeammate("developer"), "developer should match developer*")
	assert.True(t, cfg.IsTeammate("developer-1"), "developer-1 should match developer*")

	// Matches per-phase teammate glob
	assert.True(t, cfg.IsTeammate("reviewer"), "reviewer should match reviewer*")
	assert.True(t, cfg.IsTeammate("reviewer-2"), "reviewer-2 should match reviewer*")

	// Does not match any pattern
	assert.False(t, cfg.IsTeammate("lead"), "lead should not match any teammate pattern")
	assert.False(t, cfg.IsTeammate("unknown-agent"), "unknown-agent should not match")

	// Nil phases
	nilCfg := &Config{}
	assert.False(t, nilCfg.IsTeammate("developer"), "nil Phases should return false")
}

func TestIsTeammate_DefaultConfig(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	assert.True(t, cfg.IsTeammate("developer"), "developer should match developer* in default config")
	assert.True(t, cfg.IsTeammate("developer-abc"), "developer-abc should match developer* in default config")
	assert.False(t, cfg.IsTeammate("lead"), "lead should not be a teammate")
	assert.True(t, cfg.IsTeammate("reviewer"), "reviewer should match reviewer* in default config")
}

// containsStr is a helper to check if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
