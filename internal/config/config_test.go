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
	// Guards are now derived from transitions; the legacy guards section has been removed.
	assert.NotNil(t, cfg.Transitions, "defaults.yaml must contain transitions (which encode guard logic)")
	assert.NotEmpty(t, cfg.Transitions, "transitions must not be empty")
}

func TestDefaultConfig_HasExpectedGuardedTransitions(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	// Transitions that must have guard checks (non-empty when expressions).
	guarded := [][2]string{
		{"PLANNING", "RESPAWN"},
		{"RESPAWN", "DEVELOPING"},
		{"DEVELOPING", "REVIEWING"},
		{"REVIEWING", "DEVELOPING"},
		{"COMMITTING", "RESPAWN"},
		{"COMMITTING", "PR_CREATION"},
		{"FEEDBACK", "COMPLETE"},
		{"FEEDBACK", "RESPAWN"},
	}
	for _, pair := range guarded {
		rules := FindGuards(cfg, pair[0], pair[1])
		assert.NotEmpty(t, rules, "expected guard rule for %s→%s", pair[0], pair[1])
	}

	// PR_CREATION→FEEDBACK has an empty when (ci_passed check is commented out) — no guard.
	rules := FindGuards(cfg, "PR_CREATION", "FEEDBACK")
	assert.Empty(t, rules, "PR_CREATION→FEEDBACK should have no guard (when is empty)")
}

func TestLoadConfig_NoProjectFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	require.NoError(t, err)
	// Guards are now derived from transitions; verify transitions are present.
	assert.NotEmpty(t, cfg.Transitions, "transitions must be present in default config")
}

func TestLoadConfig_MergesProjectFile(t *testing.T) {
	dir := t.TempDir()
	overrideYAML := []byte(`
guards:
  - from: DEVELOPING
    to: REVIEWING
    disabled: true
`)
	require.NoError(t, os.MkdirAll(dir+"/.wf-agents", 0755))
	require.NoError(t, os.WriteFile(dir+"/.wf-agents/workflow.yaml", overrideYAML, 0644))

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
		"lint": {Patterns: []string{"npm run lint"}},  // replaces base lint
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
		Key:          "review_approved",
		Value:        "true",
		Alternatives: []KV{{Key: "merged", Value: "true"}},
		Message:      "not approved or merged",
	}
	// Primary fails, alternative passes
	ctx := &simpleCtx{evidence: map[string]string{"review_approved": "false", "merged": "true"}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_EvidenceAlternativeFail(t *testing.T) {
	c := Check{
		Type:         "evidence",
		Key:          "review_approved",
		Value:        "true",
		Alternatives: []KV{{Key: "merged", Value: "true"}},
		Message:      "not approved or merged",
	}
	ctx := &simpleCtx{evidence: map[string]string{"review_approved": "false", "merged": "false"}}
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

func TestEvalCheck_SendMessagePass(t *testing.T) {
	c := Check{Type: "send_message", Message: "Send your completion summary to the Team Lead via SendMessage before going idle."}
	ctx := &simpleCtx{commandsRan: map[string]bool{"_sent_message": true}}
	assert.Empty(t, EvalCheck(c, ctx))
}

func TestEvalCheck_SendMessageFail_NotSent(t *testing.T) {
	c := Check{Type: "send_message", Message: "Send your completion summary to the Team Lead via SendMessage before going idle."}
	ctx := &simpleCtx{commandsRan: map[string]bool{"_sent_message": false}}
	assert.Equal(t, "Send your completion summary to the Team Lead via SendMessage before going idle.", EvalCheck(c, ctx))
}

func TestEvalCheck_SendMessageFail_NilMap(t *testing.T) {
	c := Check{Type: "send_message", Message: "Send your completion summary to the Team Lead via SendMessage before going idle."}
	ctx := &simpleCtx{commandsRan: nil}
	assert.Equal(t, "Send your completion summary to the Team Lead via SendMessage before going idle.", EvalCheck(c, ctx))
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
	cfg, err := DefaultConfig()
	require.NoError(t, err)

	// team-lead has no idle checks in any phase — idle is always allowed.
	for _, phase := range []string{"DEVELOPING", "REVIEWING", "COMMITTING"} {
		rule := FindIdleRule(cfg, phase, "team-lead")
		if rule != nil {
			ctx := &simpleCtx{}
			reason := EvalChecks(rule.Checks, ctx)
			assert.Empty(t, reason, "default config should allow idle for team-lead in %s", phase)
		}
	}

	// reviewer-1 in REVIEWING is required to send a completion summary before idling.
	reviewerReviewingRule := FindIdleRule(cfg, "REVIEWING", "reviewer-1")
	require.NotNil(t, reviewerReviewingRule, "expected idle rule for reviewer-1 in REVIEWING")
	reviewerCtx := &simpleCtx{}
	reviewerReason := EvalChecks(reviewerReviewingRule.Checks, reviewerCtx)
	assert.NotEmpty(t, reviewerReason, "reviewer-1 in REVIEWING should be denied idle (send_message check)")

	// reviewer-1 in other phases (DEVELOPING, COMMITTING) should idle freely.
	for _, phase := range []string{"DEVELOPING", "COMMITTING"} {
		rule := FindIdleRule(cfg, phase, "reviewer-1")
		if rule != nil {
			ctx := &simpleCtx{}
			reason := EvalChecks(rule.Checks, ctx)
			assert.Empty(t, reason, "default config should allow idle for reviewer-1 in %s", phase)
		}
	}

	// developer-1 in DEVELOPING has lint+test checks from the phases config.
	devDevelopingRule := FindIdleRule(cfg, "DEVELOPING", "developer-1")
	require.NotNil(t, devDevelopingRule, "expected idle rule for developer-1 in DEVELOPING")
	assert.NotEmpty(t, devDevelopingRule.Checks, "developer* in DEVELOPING should have idle checks (lint+test)")

	// developer-1 in other phases should allow idle freely (no checks or nil rule).
	for _, phase := range []string{"REVIEWING", "COMMITTING"} {
		rule := FindIdleRule(cfg, phase, "developer-1")
		if rule != nil {
			ctx := &simpleCtx{}
			reason := EvalChecks(rule.Checks, ctx)
			assert.Empty(t, reason, "default config should allow idle for developer-1 in %s", phase)
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
	// Teammate permissions are now derived from phases config; legacy section was removed.
	// Verify via FindTeammatePermission which reads from phases config.

	// developer* should have Edit restricted; allowed in DEVELOPING
	rule := FindTeammatePermission(cfg, "DEVELOPING", "developer-1", "Edit", "")
	require.NotNil(t, rule, "developer* Edit should have a permission rule")
	allowed := false
	for _, p := range rule.Phases {
		if p == "DEVELOPING" {
			allowed = true
		}
	}
	assert.True(t, allowed, "Edit is allowed in DEVELOPING")

	// Edit should NOT be allowed in REVIEWING
	rule = FindTeammatePermission(cfg, "REVIEWING", "developer-1", "Edit", "")
	require.NotNil(t, rule, "developer* Edit should have a permission rule in REVIEWING")
	reviewingAllowed := false
	for _, p := range rule.Phases {
		if p == "REVIEWING" {
			reviewingAllowed = true
		}
	}
	assert.False(t, reviewingAllowed, "Edit should not be allowed in REVIEWING")
}

func TestDefaultConfig_HasLeadIdleRules(t *testing.T) {
	cfg, err := DefaultConfig()
	require.NoError(t, err)
	// Lead idle rules are now derived from phases config; legacy section was removed.
	// Verify via FindLeadIdleRule which reads from phases config.

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

// --- mergePhases ---

func TestMergePhases_OverrideFieldsPerPhase(t *testing.T) {
	base := &PhasesConfig{
		Start: "PLANNING",
		Stop:  []string{"COMPLETE"},
		Phases: map[string]PhaseConfig{
			"PLANNING": {
				Display:      PhaseDisplay{Label: "Planning", Icon: "clipboard", Color: "#111"},
				Instructions: "planning.md",
				Hint:         "base hint",
			},
		},
	}
	override := &PhasesConfig{
		Phases: map[string]PhaseConfig{
			"PLANNING": {
				Display: PhaseDisplay{Label: "Plan", Color: "#222"},
				Hint:    "override hint",
			},
		},
	}
	result := mergePhases(base, override)
	require.NotNil(t, result)
	assert.Equal(t, "PLANNING", result.Start)
	assert.Equal(t, []string{"COMPLETE"}, result.Stop)
	pc := result.Phases["PLANNING"]
	assert.Equal(t, "Plan", pc.Display.Label, "label should be overridden")
	assert.Equal(t, "clipboard", pc.Display.Icon, "icon should be preserved from base")
	assert.Equal(t, "#222", pc.Display.Color, "color should be overridden")
	assert.Equal(t, "planning.md", pc.Instructions, "instructions should be preserved from base")
	assert.Equal(t, "override hint", pc.Hint, "hint should be overridden")
}

func TestMergePhases_NewPhaseAdded(t *testing.T) {
	base := &PhasesConfig{
		Phases: map[string]PhaseConfig{
			"PLANNING": {Display: PhaseDisplay{Label: "Planning"}},
		},
	}
	override := &PhasesConfig{
		Phases: map[string]PhaseConfig{
			"CUSTOM": {Display: PhaseDisplay{Label: "Custom Phase"}, Instructions: "custom.md"},
		},
	}
	result := mergePhases(base, override)
	require.NotNil(t, result)
	assert.Contains(t, result.Phases, "PLANNING")
	require.Contains(t, result.Phases, "CUSTOM")
	assert.Equal(t, "Custom Phase", result.Phases["CUSTOM"].Display.Label)
}

func TestMergePhases_StartStopOverride(t *testing.T) {
	base := &PhasesConfig{Start: "PLANNING", Stop: []string{"COMPLETE"}}
	override := &PhasesConfig{Start: "CUSTOM", Stop: []string{"DONE"}}
	result := mergePhases(base, override)
	assert.Equal(t, "CUSTOM", result.Start)
	assert.Equal(t, []string{"DONE"}, result.Stop)
}

func TestMergePhases_NilOverride(t *testing.T) {
	base := &PhasesConfig{Start: "PLANNING"}
	result := mergePhases(base, nil)
	assert.Equal(t, base, result)
}

// --- mergeTransitions ---

func TestMergeTransitions_OverrideReplacesSourcePhase(t *testing.T) {
	base := map[string][]TransitionConfig{
		"PLANNING": {{To: "RESPAWN"}},
		"RESPAWN":  {{To: "DEVELOPING"}},
	}
	override := map[string][]TransitionConfig{
		"PLANNING": {{To: "CUSTOM"}, {To: "BLOCKED"}},
	}
	result := mergeTransitions(base, override)
	assert.Equal(t, []TransitionConfig{{To: "CUSTOM"}, {To: "BLOCKED"}}, result["PLANNING"])
	assert.Equal(t, []TransitionConfig{{To: "DEVELOPING"}}, result["RESPAWN"], "unaffected phase preserved")
}

func TestMergeTransitions_NewSourcePhaseAdded(t *testing.T) {
	base := map[string][]TransitionConfig{
		"PLANNING": {{To: "RESPAWN"}},
	}
	override := map[string][]TransitionConfig{
		"CUSTOM": {{To: "DONE"}},
	}
	result := mergeTransitions(base, override)
	assert.Contains(t, result, "PLANNING")
	require.Contains(t, result, "CUSTOM")
	assert.Equal(t, []TransitionConfig{{To: "DONE"}}, result["CUSTOM"])
}

func TestMergeTransitions_NilOverride(t *testing.T) {
	base := map[string][]TransitionConfig{
		"PLANNING": {{To: "RESPAWN"}},
	}
	result := mergeTransitions(base, nil)
	assert.Equal(t, base, result)
}

// --- MergeConfigs includes phases and transitions ---

func TestMergeConfigs_PhasesAndTransitions(t *testing.T) {
	base := &Config{
		Phases: &PhasesConfig{
			Start: "PLANNING",
			Phases: map[string]PhaseConfig{
				"PLANNING": {Hint: "base"},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"PLANNING": {{To: "RESPAWN"}},
		},
	}
	override := &Config{
		Phases: &PhasesConfig{
			Phases: map[string]PhaseConfig{
				"PLANNING": {Hint: "override"},
			},
		},
		Transitions: map[string][]TransitionConfig{
			"PLANNING": {{To: "CUSTOM"}},
		},
	}
	result := MergeConfigs(base, override)
	assert.Equal(t, "override", result.Phases.Phases["PLANNING"].Hint)
	assert.Equal(t, []TransitionConfig{{To: "CUSTOM"}}, result.Transitions["PLANNING"])
}
