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
		{"REVIEWING", "RESPAWN"},
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
		{Match: "*", Checks: []Check{}},
	}}
	override := &Config{TeammateIdle: []IdleRule{
		{Match: "*", Checks: []Check{{Type: "evidence", Key: "done", Value: "true", Message: "not done"}}},
	}}

	merged := MergeConfigs(base, override)
	require.Len(t, merged.TeammateIdle, 1)
	assert.Len(t, merged.TeammateIdle[0].Checks, 1, "idle rule should be replaced")
}

func TestMergeConfigs_IdleNewMatchAppended(t *testing.T) {
	base := &Config{TeammateIdle: []IdleRule{{Match: "*", Checks: []Check{}}}}
	override := &Config{TeammateIdle: []IdleRule{{Match: "DEVELOPING", Checks: []Check{{Type: "no_active_agents", Message: "x"}}}}}

	merged := MergeConfigs(base, override)
	assert.Len(t, merged.TeammateIdle, 2)
}

// --- Check evaluation ---

// simpleCtx is a test implementation of CheckContext.
type simpleCtx struct {
	evidence     map[string]string
	agentCount   int
	iteration    int
	maxIter      int
	originPhase  string
	commandsRan  map[string]bool
	teammateName string
}

func (c *simpleCtx) Evidence() map[string]string  { return c.evidence }
func (c *simpleCtx) ActiveAgentCount() int        { return c.agentCount }
func (c *simpleCtx) Iteration() int               { return c.iteration }
func (c *simpleCtx) MaxIterations() int           { return c.maxIter }
func (c *simpleCtx) OriginPhase() string          { return c.originPhase }
func (c *simpleCtx) CommandsRan() map[string]bool { return c.commandsRan }
func (c *simpleCtx) TeammateName() string         { return c.teammateName }

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
	c := Check{Type: "command_ran", Key: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"test": true}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanFail_NilMap(t *testing.T) {
	c := Check{Type: "command_ran", Key: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: nil}
	assert.Equal(t, "tests not run", EvalCheck(c, ctx))
}

func TestEvalCheck_CommandRanFail_CategoryNotSet(t *testing.T) {
	c := Check{Type: "command_ran", Key: "test", Message: "tests not run"}
	ctx := &simpleCtx{commandsRan: map[string]bool{"lint": true}}
	assert.Equal(t, "tests not run", EvalCheck(c, ctx))
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

// --- role_check ---

func TestEvalCheck_RoleCheckMatchFails(t *testing.T) {
	c := Check{Type: "role_check", Key: "developer", Message: "developer blocked"}
	ctx := &simpleCtx{teammateName: "developer-2"}
	assert.Equal(t, "developer blocked", EvalCheck(c, ctx))
}

func TestEvalCheck_RoleCheckNoMatchPasses(t *testing.T) {
	c := Check{Type: "role_check", Key: "developer", Message: "developer blocked"}
	ctx := &simpleCtx{teammateName: "reviewer-1"}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_RoleCheckCaseInsensitive(t *testing.T) {
	c := Check{Type: "role_check", Key: "Developer", Message: "blocked"}
	ctx := &simpleCtx{teammateName: "Developer-1"}
	assert.Equal(t, "blocked", EvalCheck(c, ctx))
}

// --- FindIdleRule ---

func TestFindIdleRule_ExactMatch(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Match: "DEVELOPING", Checks: []Check{{Type: "role_check", Key: "developer", Message: "m"}}},
		{Match: "*", Checks: []Check{}},
	}}
	rule := FindIdleRule(cfg, "DEVELOPING")
	require.NotNil(t, rule)
	assert.Equal(t, "DEVELOPING", rule.Match)
}

func TestFindIdleRule_WildcardFallback(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Match: "DEVELOPING", Checks: []Check{{Type: "role_check", Key: "developer", Message: "m"}}},
		{Match: "*", Checks: []Check{}},
	}}
	rule := FindIdleRule(cfg, "REVIEWING")
	require.NotNil(t, rule)
	assert.Equal(t, "*", rule.Match)
}

func TestFindIdleRule_NoMatch(t *testing.T) {
	cfg := &Config{TeammateIdle: []IdleRule{
		{Match: "DEVELOPING", Checks: []Check{{Type: "role_check", Key: "developer", Message: "m"}}},
	}}
	rule := FindIdleRule(cfg, "REVIEWING")
	assert.Nil(t, rule)
}

func TestFindIdleRule_DefaultConfigDeveloperInDeveloping(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	rule := FindIdleRule(cfg, "DEVELOPING")
	require.NotNil(t, rule)
	ctx := &simpleCtx{teammateName: "developer-2"}
	reason := EvalChecks(rule.Checks, ctx)
	assert.NotEmpty(t, reason)
}

func TestFindIdleRule_DefaultConfigReviewerInDeveloping(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	rule := FindIdleRule(cfg, "DEVELOPING")
	require.NotNil(t, rule)
	ctx := &simpleCtx{teammateName: "reviewer-1"}
	reason := EvalChecks(rule.Checks, ctx)
	assert.Empty(t, reason)
}

func TestFindIdleRule_DefaultConfigAnyPhaseWildcard(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	rule := FindIdleRule(cfg, "REVIEWING")
	require.NotNil(t, rule)
	ctx := &simpleCtx{teammateName: "developer-1"}
	reason := EvalChecks(rule.Checks, ctx)
	assert.Empty(t, reason)
}
