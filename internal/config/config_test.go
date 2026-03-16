package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DefaultConfig + LoadConfig ---

func TestDefaultConfig_ParsesSuccessfully(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Guards, "defaults.yaml must contain guard rules")
}

func TestDefaultConfig_HasExpectedGuardedTransitions(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	// Every guarded transition from the original hardcoded guards.go must appear.
	expected := [][2]string{
		{"RESPAWN", "DEVELOPING"},
		{"DEVELOPING", "REVIEWING"},
		{"REVIEWING", "DEVELOPING"},
		{"COMMITTING", "RESPAWN"},
		{"COMMITTING", "PR_CREATION"},
		{"PR_CREATION", "FEEDBACK"},
		{"FEEDBACK", "COMPLETE"},
		{"FEEDBACK", "RESPAWN"},
	}

	for _, pair := range expected {
		rules := FindGuards(cfg, pair[0], pair[1])
		assert.NotEmpty(t, rules, "expected guard rule for %s→%s", pair[0], pair[1])
	}
}

func TestLoadConfig_NoProjectFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Guards)
}

func TestLoadConfig_MergesProjectFile(t *testing.T) {
	dir := t.TempDir()
	overrideYAML := []byte(`
guards:
  - from: DEVELOPING
    to: REVIEWING
    disabled: true
`)
	require.NoError(t, os.WriteFile(dir+"/.wf-agents.yaml", overrideYAML, 0644))

	cfg, err := LoadConfig(dir)
	require.NoError(t, err)

	// DEVELOPING→REVIEWING should be disabled (removed)
	rules := FindGuards(cfg, "DEVELOPING", "REVIEWING")
	assert.Empty(t, rules, "disabled guard should be removed after merge")
}

// --- Merge rules ---

func TestMergeConfigs_TrackingOverrideByKey(t *testing.T) {
	base := &Config{Tracking: TrackingConfig{
		"lint": {Patterns: []string{"go vet", "golangci-lint"}},
		"test": {Patterns: []string{"go test"}},
	}}
	override := &Config{Tracking: TrackingConfig{
		"lint": {Patterns: []string{"npm run lint"}}, // replaces base lint
		"e2e":  {Patterns: []string{"make test-e2e"}}, // new key appended
	}}

	merged := MergeConfigs(base, override)

	// Override replaces base for same key
	assert.Equal(t, []string{"npm run lint"}, merged.Tracking["lint"].Patterns)
	// Base-only key preserved
	assert.Equal(t, []string{"go test"}, merged.Tracking["test"].Patterns)
	// New key appended
	assert.Equal(t, []string{"make test-e2e"}, merged.Tracking["e2e"].Patterns)
}

func TestTrackingCategory_ShouldInvalidateOnFileChange_DefaultTrue(t *testing.T) {
	tc := TrackingCategory{Patterns: []string{"go test"}}
	assert.True(t, tc.ShouldInvalidateOnFileChange(), "default should be true when nil")
}

func TestTrackingCategory_ShouldInvalidateOnFileChange_ExplicitFalse(t *testing.T) {
	f := false
	tc := TrackingCategory{Patterns: []string{"go test"}, InvalidateOnFileChange: &f}
	assert.False(t, tc.ShouldInvalidateOnFileChange())
}

func TestIsFileChangeTool(t *testing.T) {
	assert.True(t, IsFileChangeTool("Edit"))
	assert.True(t, IsFileChangeTool("Write"))
	assert.True(t, IsFileChangeTool("NotebookEdit"))
	assert.False(t, IsFileChangeTool("Bash"))
	assert.False(t, IsFileChangeTool("Read"))
}

func TestMergeConfigs_GuardsAppendChecks(t *testing.T) {
	base := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	override := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "no_active_agents", Message: "extra"}}},
	}}

	merged := MergeConfigs(base, override)
	require.Len(t, merged.Guards, 1)
	assert.Len(t, merged.Guards[0].Checks, 2, "checks should be appended")
}

func TestMergeConfigs_GuardsDisabled(t *testing.T) {
	base := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	override := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Disabled: true},
	}}

	merged := MergeConfigs(base, override)
	rules := FindGuards(merged, "DEVELOPING", "REVIEWING")
	assert.Empty(t, rules, "disabled guard should be removed")
}

func TestMergeConfigs_GuardsNewPairAppended(t *testing.T) {
	base := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	override := &Config{Guards: []GuardRule{
		{From: "RESPAWN", To: "DEVELOPING", Checks: []Check{{Type: "no_active_agents", Message: "extra"}}},
	}}

	merged := MergeConfigs(base, override)
	assert.Len(t, merged.Guards, 2)
}

func TestMergeConfigs_IdleOverride(t *testing.T) {
	base := &Config{TeammateIdle: []IdleRule{
		{Phase: "*", Checks: []Check{}},
	}}
	override := &Config{TeammateIdle: []IdleRule{
		{Phase: "*", Checks: []Check{{Type: "evidence", Key: "done", Value: "true", Message: "not done"}}},
	}}

	merged := MergeConfigs(base, override)
	require.Len(t, merged.TeammateIdle, 1)
	assert.Len(t, merged.TeammateIdle[0].Checks, 1, "idle rule should be replaced")
}

func TestMergeConfigs_IdleNewMatchAppended(t *testing.T) {
	base := &Config{TeammateIdle: []IdleRule{{Phase: "*", Checks: []Check{}}}}
	override := &Config{TeammateIdle: []IdleRule{{Phase: "DEVELOPING", Checks: []Check{{Type: "no_active_agents", Message: "x"}}}}}

	merged := MergeConfigs(base, override)
	assert.Len(t, merged.TeammateIdle, 2)
}

// --- Check evaluation ---

// simpleCtx is a test implementation of CheckContext.
type simpleCtx struct {
	evidence    map[string]string
	agentCount  int
	iteration   int
	maxIter     int
	originPhase string
	commandsRan map[string]bool
}

func (c *simpleCtx) Evidence() map[string]string  { return c.evidence }
func (c *simpleCtx) ActiveAgentCount() int        { return c.agentCount }
func (c *simpleCtx) Iteration() int               { return c.iteration }
func (c *simpleCtx) MaxIterations() int           { return c.maxIter }
func (c *simpleCtx) OriginPhase() string          { return c.originPhase }
func (c *simpleCtx) CommandsRan() map[string]bool { return c.commandsRan }

func TestEvalCheck_EvidencePass(t *testing.T) {
	c := Check{Type: "evidence", Key: "k", Value: "v", Message: "failed"}
	ctx := &simpleCtx{evidence: map[string]string{"k": "v"}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_EvidenceFail(t *testing.T) {
	c := Check{Type: "evidence", Key: "k", Value: "v", Message: "failed"}
	ctx := &simpleCtx{evidence: map[string]string{"k": "x"}}
	assert.Equal(t, "failed", EvalCheck(c, ctx))
}

func TestEvalCheck_EvidenceAlternativePass(t *testing.T) {
	c := Check{
		Type:         "evidence",
		Key:          "pr_approved",
		Value:        "true",
		Alternatives: []KV{{Key: "pr_merged", Value: "true"}},
		Message:      "not approved or merged",
	}
	// Primary fails, alternative passes
	ctx := &simpleCtx{evidence: map[string]string{"pr_approved": "false", "pr_merged": "true"}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_EvidenceAlternativeFail(t *testing.T) {
	c := Check{
		Type:         "evidence",
		Key:          "pr_approved",
		Value:        "true",
		Alternatives: []KV{{Key: "pr_merged", Value: "true"}},
		Message:      "not approved or merged",
	}
	ctx := &simpleCtx{evidence: map[string]string{"pr_approved": "false", "pr_merged": "false"}}
	assert.Equal(t, "not approved or merged", EvalCheck(c, ctx))
}

func TestEvalCheck_NoActiveAgentsPass(t *testing.T) {
	c := Check{Type: "no_active_agents", Message: "agents still active"}
	ctx := &simpleCtx{agentCount: 0}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_NoActiveAgentsFail(t *testing.T) {
	c := Check{Type: "no_active_agents", Message: "agents still active"}
	ctx := &simpleCtx{agentCount: 2}
	reason := EvalCheck(c, ctx)
	assert.NotEmpty(t, reason)
	assert.Contains(t, reason, "agents still active")
}

func TestEvalCheck_MaxIterationsPlanningExempt(t *testing.T) {
	c := Check{Type: "max_iterations", Message: "max reached"}
	ctx := &simpleCtx{originPhase: "PLANNING", iteration: 99, maxIter: 1}
	assert.Empty(t, EvalCheck(c, ctx), "PLANNING origin must be exempt from max_iterations")
}

func TestEvalCheck_MaxIterationsPass(t *testing.T) {
	c := Check{Type: "max_iterations", Message: "max reached"}
	ctx := &simpleCtx{originPhase: "DEVELOPING", iteration: 1, maxIter: 5}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_MaxIterationsFail(t *testing.T) {
	c := Check{Type: "max_iterations", Message: "max reached"}
	ctx := &simpleCtx{originPhase: "DEVELOPING", iteration: 5, maxIter: 5}
	reason := EvalCheck(c, ctx)
	assert.NotEmpty(t, reason)
}

func TestEvalCheck_CommandRanPass(t *testing.T) {
	c := Check{Type: "command_ran", Category: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"test": true}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanFail_NilMap(t *testing.T) {
	c := Check{Type: "command_ran", Category: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: nil}
	assert.Equal(t, "tests not run", EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanFail_CategoryNotSet(t *testing.T) {
	c := Check{Type: "command_ran", Category: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"lint": true}}
	assert.Equal(t, "tests not run", EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanPass_Category(t *testing.T) {
	c := Check{Type: "command_ran", Category: "lint", Message: "lint not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"lint": true}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanFail_Category(t *testing.T) {
	c := Check{Type: "command_ran", Category: "lint", Message: "lint not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"test": true}}
	assert.Equal(t, "lint not run", EvalCheck(c, ctx))
}

func TestEvalCheck_UnknownTypeFailsSafe(t *testing.T) {
	c := Check{Type: "totally_unknown"}
	ctx := &simpleCtx{}
	reason := EvalCheck(c, ctx)
	assert.Contains(t, reason, "unknown check type")
}

func TestEvalChecks_ShortCircuit(t *testing.T) {
	checks := []Check{
		{Type: "evidence", Key: "a", Value: "1", Message: "a failed"},
		{Type: "evidence", Key: "b", Value: "2", Message: "b failed"},
	}
	ctx := &simpleCtx{evidence: map[string]string{"a": "X", "b": "2"}}
	assert.Equal(t, "a failed", EvalChecks(checks, ctx))
}

func TestEvalChecks_AllPass(t *testing.T) {
	checks := []Check{
		{Type: "evidence", Key: "a", Value: "1", Message: "a failed"},
		{Type: "evidence", Key: "b", Value: "2", Message: "b failed"},
	}
	ctx := &simpleCtx{evidence: map[string]string{"a": "1", "b": "2"}}
	assert.Empty(t, EvalChecks(checks, ctx))
}

// --- FindGuards ---

func TestFindGuards_ExactMatch(t *testing.T) {
	cfg := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	rules := FindGuards(cfg, "DEVELOPING", "REVIEWING")
	assert.Len(t, rules, 1)
}

func TestFindGuards_NoMatch(t *testing.T) {
	cfg := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	rules := FindGuards(cfg, "PLANNING", "RESPAWN")
	assert.Empty(t, rules)
}

func TestFindGuards_WildcardFrom(t *testing.T) {
	cfg := &Config{Guards: []GuardRule{
		{From: "*", To: "BLOCKED", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	rules := FindGuards(cfg, "DEVELOPING", "BLOCKED")
	assert.Len(t, rules, 1)
}

func TestFindGuards_WildcardTo(t *testing.T) {
	cfg := &Config{Guards: []GuardRule{
		{From: "DEVELOPING", To: "*", Checks: []Check{{Type: "evidence", Key: "k", Value: "v", Message: "m"}}},
	}}
	rules := FindGuards(cfg, "DEVELOPING", "REVIEWING")
	assert.Len(t, rules, 1)
}

func TestFindGuards_ExactBeforeWildcard(t *testing.T) {
	cfg := &Config{Guards: []GuardRule{
		{From: "*", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "wild", Value: "1", Message: "wild"}}},
		{From: "DEVELOPING", To: "REVIEWING", Checks: []Check{{Type: "evidence", Key: "exact", Value: "1", Message: "exact"}}},
	}}
	rules := FindGuards(cfg, "DEVELOPING", "REVIEWING")
	require.Len(t, rules, 2)
	// Exact match must come first
	assert.Equal(t, "DEVELOPING", rules[0].From)
	assert.Equal(t, "*", rules[1].From)
}

// --- FindIdleRule ---

func TestFindIdleRule_ExactMatch(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Checks: []Check{{Type: "command_ran", Category: "test", Message: "m"}}},
		{Phase: "*", Checks: []Check{}},
	}}
	rule := FindIdleRule(cfg, "DEVELOPING", "developer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "DEVELOPING", rule.Phase)
}

func TestFindIdleRule_WildcardFallback(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Checks: []Check{{Type: "command_ran", Category: "test", Message: "m"}}},
		{Phase: "*", Checks: []Check{}},
	}}
	rule := FindIdleRule(cfg, "REVIEWING", "developer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "*", rule.Phase)
}

func TestFindIdleRule_NoMatch(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Checks: []Check{{Type: "command_ran", Category: "test", Message: "m"}}},
	}}
	rule := FindIdleRule(cfg, "REVIEWING", "developer-1")
	assert.Nil(t, rule)
}

func TestFindIdleRule_AgentGlobMatch(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "run tests"}}},
		{Phase: "DEVELOPING", Agent: "reviewer*", Checks: []Check{}},
		{Phase: "*", Checks: []Check{}},
	}}

	// developer-1 matches developer* rule
	rule := FindIdleRule(cfg, "DEVELOPING", "developer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "developer*", rule.Agent)

	// reviewer-1 matches reviewer* rule
	rule = FindIdleRule(cfg, "DEVELOPING", "reviewer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "reviewer*", rule.Agent)

	// unknown agent falls through to wildcard
	rule = FindIdleRule(cfg, "DEVELOPING", "other-agent")
	require.NotNil(t, rule)
	assert.Equal(t, "*", rule.Phase)
}

func TestFindIdleRule_AgentGlobCaseInsensitive(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "Developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "run tests"}}},
		{Phase: "*", Checks: []Check{}},
	}}
	rule := FindIdleRule(cfg, "DEVELOPING", "developer-2")
	require.NotNil(t, rule)
	assert.Equal(t, "Developer*", rule.Agent)
}

func TestFindIdleRule_ExactAgentBeatsNoAgent(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Checks: []Check{{Type: "command_ran", Category: "lint", Message: "run lint"}}},
		{Phase: "DEVELOPING", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "run tests"}}},
	}}
	// developer* agent rule should win over no-agent rule
	rule := FindIdleRule(cfg, "DEVELOPING", "developer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "developer*", rule.Agent)
}

func TestFindIdleRule_WildcardAgentBeatsWildcardNoAgent(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Phase: "*", Checks: []Check{}},
		{Phase: "*", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "run tests"}}},
	}}
	rule := FindIdleRule(cfg, "REVIEWING", "developer-1")
	require.NotNil(t, rule)
	assert.Equal(t, "developer*", rule.Agent)
}

func TestFindIdleRule_DefaultConfigAllowsIdleByDefault(t *testing.T) {
	// Default config has only a wildcard rule with no checks — all agents idle freely.
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	for _, agent := range []string{"developer-1", "reviewer-1", "team-lead"} {
		for _, phase := range []string{"DEVELOPING", "REVIEWING", "COMMITTING"} {
			rule := FindIdleRule(cfg, phase, agent)
			require.NotNil(t, rule, "expected wildcard rule for %s in %s", agent, phase)
			ctx := &simpleCtx{}
			reason := EvalChecks(rule.Checks, ctx)
			assert.Empty(t, reason, "default config should allow idle for %s in %s", agent, phase)
		}
	}
}

// --- Merge with agent field ---

func TestMergeConfigs_IdleAgentFieldCoexists(t *testing.T) {
	base := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "run tests"}}},
		{Phase: "*", Checks: []Check{}},
	}}
	override := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "reviewer*", Checks: []Check{}},
	}}

	merged := MergeConfigs(base, override)
	// Both DEVELOPING rules should coexist (different agent patterns)
	var devRule, revRule *IdleRule
	for i := range merged.TeammateIdle {
		r := &merged.TeammateIdle[i]
		if r.Phase == "DEVELOPING" && r.Agent == "developer*" {
			devRule = r
		}
		if r.Phase == "DEVELOPING" && r.Agent == "reviewer*" {
			revRule = r
		}
	}
	require.NotNil(t, devRule, "developer* rule should be present")
	require.NotNil(t, revRule, "reviewer* rule should be present")
}

func TestMergeConfigs_IdleSameMatchAndAgentReplaces(t *testing.T) {
	base := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "test", Message: "original"}}},
	}}
	override := &Config{TeammateIdle: []IdleRule{
		{Phase: "DEVELOPING", Agent: "developer*", Checks: []Check{{Type: "command_ran", Category: "lint", Message: "replaced"}}},
	}}

	merged := MergeConfigs(base, override)
	require.Len(t, merged.TeammateIdle, 1)
	require.Len(t, merged.TeammateIdle[0].Checks, 1)
	assert.Equal(t, "lint", merged.TeammateIdle[0].Checks[0].Category)
}

// --- FindLeadIdleRule ---

func TestFindLeadIdleRule(t *testing.T) {
	cfg := &Config{
		LeadIdle: []LeadIdleRule{
			{Phase: "PLANNING", Deny: true, Message: "No teammates in PLANNING"},
			{Phase: "FEEDBACK", Deny: true, Message: "Continue polling"},
			{Phase: "*", Deny: false},
		},
	}

	// Exact match: PLANNING
	r := FindLeadIdleRule(cfg, "PLANNING")
	require.NotNil(t, r)
	assert.True(t, r.Deny)
	assert.Equal(t, "No teammates in PLANNING", r.Message)

	// Exact match: FEEDBACK
	r = FindLeadIdleRule(cfg, "FEEDBACK")
	require.NotNil(t, r)
	assert.True(t, r.Deny)
	assert.Equal(t, "Continue polling", r.Message)

	// Wildcard match: DEVELOPING falls through to "*"
	r = FindLeadIdleRule(cfg, "DEVELOPING")
	require.NotNil(t, r)
	assert.False(t, r.Deny)

	// No rules at all → nil
	empty := &Config{}
	r = FindLeadIdleRule(empty, "PLANNING")
	assert.Nil(t, r)
}

func TestFindLeadIdleRulePriority(t *testing.T) {
	cfg := &Config{
		LeadIdle: []LeadIdleRule{
			{Phase: "*", Deny: false, Message: "wildcard"},
			{Phase: "PLANNING", Deny: true, Message: "exact"},
		},
	}

	// Exact match must win over wildcard even when wildcard appears first
	r := FindLeadIdleRule(cfg, "PLANNING")
	require.NotNil(t, r)
	assert.Equal(t, "exact", r.Message, "exact phase match should take priority over wildcard")

	// Phase with no exact rule falls back to wildcard
	r = FindLeadIdleRule(cfg, "REVIEWING")
	require.NotNil(t, r)
	assert.Equal(t, "wildcard", r.Message)
}

func TestMergeConfigsPreservesLeadIdle(t *testing.T) {
	base := &Config{
		LeadIdle: []LeadIdleRule{
			{Phase: "PLANNING", Deny: true, Message: "No teammates in PLANNING"},
			{Phase: "*", Deny: false},
		},
	}
	override := &Config{} // no LeadIdle in override

	merged := MergeConfigs(base, override)
	require.Len(t, merged.LeadIdle, 2, "base LeadIdle rules should be preserved when override has none")

	r := FindLeadIdleRule(merged, "PLANNING")
	require.NotNil(t, r)
	assert.True(t, r.Deny)
	assert.Equal(t, "No teammates in PLANNING", r.Message)

	r = FindLeadIdleRule(merged, "DEVELOPING")
	require.NotNil(t, r)
	assert.False(t, r.Deny)
}

func TestMergeConfigsOverridesLeadIdle(t *testing.T) {
	base := &Config{
		LeadIdle: []LeadIdleRule{
			{Phase: "PLANNING", Deny: true, Message: "base message"},
			{Phase: "*", Deny: false},
		},
	}
	override := &Config{
		LeadIdle: []LeadIdleRule{
			{Phase: "PLANNING", Deny: false, Message: "override allows planning"},
		},
	}

	merged := MergeConfigs(base, override)
	r := FindLeadIdleRule(merged, "PLANNING")
	require.NotNil(t, r)
	assert.False(t, r.Deny, "override should replace base for same phase")
	assert.Equal(t, "override allows planning", r.Message)
}

// --- FindTeammatePermission ---

func TestFindTeammatePermission_ToolMatch(t *testing.T) {
	cfg := &Config{
		TeammatePermissions: []TeammatePermission{
			{Agent: "developer*", Tools: []string{"Edit", "Write"}, Phases: []string{"DEVELOPING"}, Message: "edit only in DEVELOPING"},
		},
	}
	// Matching tool
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	require.NotNil(t, rule)
	assert.Equal(t, "edit only in DEVELOPING", rule.Message)

	// Non-matching tool
	rule = FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Bash", "")
	assert.Nil(t, rule)
}

func TestFindTeammatePermission_AgentGlob(t *testing.T) {
	cfg := &Config{
		TeammatePermissions: []TeammatePermission{
			{Agent: "developer*", Tools: []string{"Edit"}, Phases: []string{"DEVELOPING"}},
		},
	}
	// Matches developer*
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	require.NotNil(t, rule)

	// Does not match reviewer
	rule = FindTeammatePermission(cfg, "DEVELOPING", "reviewer-1", "Edit", "")
	assert.Nil(t, rule)
}

func TestFindTeammatePermission_EmptyAgentMatchesAll(t *testing.T) {
	cfg := &Config{
		TeammatePermissions: []TeammatePermission{
			{Tools: []string{"Edit"}, Phases: []string{"DEVELOPING"}},
		},
	}
	rule := FindTeammatePermission(cfg, "DEVELOPING", "reviewer-1", "Edit", "")
	require.NotNil(t, rule)
}

func TestFindTeammatePermission_BashMatch(t *testing.T) {
	cfg := &Config{
		TeammatePermissions: []TeammatePermission{
			{Agent: "developer*", Bash: []string{"git commit"}, Phases: []string{"COMMITTING"}},
		},
	}
	// Exact bash prefix match
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Bash", "git commit -m 'test'")
	require.NotNil(t, rule)

	// No match
	rule = FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Bash", "go test ./...")
	assert.Nil(t, rule)
}

func TestFindTeammatePermission_FirstMatchWins(t *testing.T) {
	cfg := &Config{
		TeammatePermissions: []TeammatePermission{
			{Agent: "developer*", Tools: []string{"Edit"}, Phases: []string{"DEVELOPING"}, Message: "first"},
			{Agent: "developer*", Tools: []string{"Edit"}, Phases: []string{"COMMITTING"}, Message: "second"},
		},
	}
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	require.NotNil(t, rule)
	assert.Equal(t, "first", rule.Message)
}

func TestFindTeammatePermission_NoRules(t *testing.T) {
	cfg := &Config{}
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	assert.Nil(t, rule)
}

func TestDefaultConfig_HasTeammatePermissions(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.TeammatePermissions, "defaults.yaml must contain teammate_permissions")

	// developer* should have Edit restricted
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	require.NotNil(t, rule)
	// Edit is allowed in DEVELOPING
	allowed := false
	for _, p := range rule.Phases {
		if p == "DEVELOPING" {
			allowed = true
		}
	}
	assert.True(t, allowed)

	// Edit should NOT be allowed in REVIEWING
	rule = FindTeammatePermission(cfg, "REVIEWING", "developer-1", "Edit", "")
	require.NotNil(t, rule)
	reviewingAllowed := false
	for _, p := range rule.Phases {
		if p == "REVIEWING" {
			reviewingAllowed = true
		}
	}
	assert.False(t, reviewingAllowed)
}

func TestDefaultConfig_HasLeadIdleRules(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.LeadIdle, "defaults.yaml must contain lead_idle rules")

	// PLANNING should be denied
	r := FindLeadIdleRule(cfg, "PLANNING")
	require.NotNil(t, r)
	assert.True(t, r.Deny, "PLANNING should deny lead idle by default")

	// FEEDBACK should be denied
	r = FindLeadIdleRule(cfg, "FEEDBACK")
	require.NotNil(t, r)
	assert.True(t, r.Deny, "FEEDBACK should deny lead idle by default")

	// DEVELOPING should allow
	r = FindLeadIdleRule(cfg, "DEVELOPING")
	require.NotNil(t, r)
	assert.False(t, r.Deny, "DEVELOPING should allow lead idle by default")
}
