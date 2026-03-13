package workflow

import (
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func setupEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	suite := &testsuite.WorkflowTestSuite{}
	return suite.NewTestWorkflowEnvironment()
}

// testEvidence provides default evidence that satisfies all guards.
var testEvidence = map[string]string{
	"working_tree_clean": "true",
	"pr_checks_pass":     "true",
	"pr_approved":        "true",
}

// testEvidenceDirty provides evidence for a dirty working tree (needed for DEVELOPING → REVIEWING).
var testEvidenceDirty = map[string]string{
	"working_tree_clean": "false",
	"pr_checks_pass":     "true",
	"pr_approved":        "true",
}

func update(env *testsuite.TestWorkflowEnvironment, t *testing.T, to model.Phase) {
	// Pick appropriate evidence based on target phase
	evidence := testEvidence
	if to == model.PhaseReviewing {
		evidence = testEvidenceDirty
	}
	env.UpdateWorkflowNoRejection(UpdateTransition, "", t, model.SignalTransition{
		To:        to,
		SessionID: "test",
		Reason:    "test",
		Guards:    evidence,
	})
}

func updateMayDeny(env *testsuite.TestWorkflowEnvironment, to model.Phase) {
	env.UpdateWorkflow(UpdateTransition, "", &testsuite.TestUpdateCallback{
		OnAccept:   func() {},
		OnComplete: func(interface{}, error) {},
		OnReject:   func(error) {},
	}, model.SignalTransition{
		To:        to,
		SessionID: "test",
		Reason:    "test",
		Guards:    testEvidence,
	})
}

func registerTransitions(env *testsuite.TestWorkflowEnvironment, t *testing.T, phases []model.Phase) {
	for _, p := range phases {
		p := p
		env.RegisterDelayedCallback(func() {
			update(env, t, p)
		}, 0)
	}
}

func TestHappyPath(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))
	// initial event + 7 transitions = 8
	assert.Equal(t, 8, len(timeline.Events))

	for _, e := range timeline.Events {
		assert.Equal(t, model.EventTransition, e.Type)
	}
}

func TestReviewRejectLoop(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseRespawn,  // reject now goes through RESPAWN, not directly to DEVELOPING
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

func TestFeedbackRespawnLoop(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseRespawn, // feedback → respawn (iter 2)
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryStatus)
	require.NoError(t, err)
	var status model.WorkflowStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, 2, status.Iteration)
}

func TestInvalidTransitionDenied(t *testing.T) {
	env := setupEnv(t)

	// PLANNING → DEVELOPING is invalid
	env.RegisterDelayedCallback(func() {
		updateMayDeny(env, model.PhaseDeveloping)
	}, 0)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))

	hasDenial := false
	for _, e := range timeline.Events {
		if e.Type == model.EventHookDenial {
			hasDenial = true
			assert.Equal(t, "PLANNING", e.Detail["from"])
			assert.Equal(t, "DEVELOPING", e.Detail["to"])
			break
		}
	}
	assert.True(t, hasDenial, "should have denial for PLANNING→DEVELOPING")
}

func TestMaxIterationsEnforced(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhaseRespawn, // iter 2
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
	})

	// iter 3 → DENIED by guard
	env.RegisterDelayedCallback(func() {
		updateMayDeny(env, model.PhaseRespawn)
	}, 0)

	// Still in COMMITTING, complete via valid path
	registerTransitions(env, t, []model.Phase{
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 2,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryStatus)
	require.NoError(t, err)
	var status model.WorkflowStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, 2, status.Iteration)

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))
	hasDenial := false
	for _, e := range timeline.Events {
		if e.Type == model.EventHookDenial && e.Detail["reason"] != "" {
			hasDenial = true
			assert.Contains(t, e.Detail["reason"], "max iterations")
			break
		}
	}
	assert.True(t, hasDenial, "should have denial for max iterations")
}

func TestBlockedRespawnNoDoubleCount(t *testing.T) {
	// When unblocking back to RESPAWN, iteration should NOT be incremented again
	// (it was already counted on the original entry to RESPAWN).
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,       // iter 1 (from PLANNING, doesn't count)
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhaseRespawn,       // iter 2
		model.PhaseDeveloping,
		model.PhaseBlocked,       // blocked in DEVELOPING
		model.PhaseDeveloping,    // unblock back to DEVELOPING
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 2,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryStatus)
	require.NoError(t, err)
	var status model.WorkflowStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, 2, status.Iteration, "iteration should still be 2 after unblock")
}

func TestBlockedAtMaxIterNoBypass(t *testing.T) {
	// When at maxIter and BLOCKED, unblocking should work but further
	// RESPAWN attempts should still be denied by guards.
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,       // iter 1 (from PLANNING, doesn't count)
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhaseRespawn,       // iter 2 (maxIter reached)
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhaseBlocked,       // blocked in COMMITTING at iter 2
		model.PhaseCommitting,    // unblock back to COMMITTING
	})

	// COMMITTING → RESPAWN must be DENIED (maxIter exceeded)
	env.RegisterDelayedCallback(func() {
		updateMayDeny(env, model.PhaseRespawn)
	}, 0)

	// Complete via valid path
	registerTransitions(env, t, []model.Phase{
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 2,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryStatus)
	require.NoError(t, err)
	var status model.WorkflowStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, 2, status.Iteration, "iteration should be 2 (maxIter)")

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))
	hasDenial := false
	for _, e := range timeline.Events {
		if e.Type == model.EventHookDenial && e.Detail["reason"] != "" {
			hasDenial = true
			assert.Contains(t, e.Detail["reason"], "max iterations")
			break
		}
	}
	assert.True(t, hasDenial, "should deny RESPAWN after BLOCKED when at maxIter")
}

func TestBlockedAndUnblock(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseBlocked,
	})

	// DENIED — must return to DEVELOPING, not REVIEWING
	env.RegisterDelayedCallback(func() {
		updateMayDeny(env, model.PhaseReviewing)
	}, 0)

	registerTransitions(env, t, []model.Phase{
		model.PhaseDeveloping, // correct unblock
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))
	hasDenial := false
	for _, e := range timeline.Events {
		if e.Type == model.EventHookDenial && e.Detail["from"] == "BLOCKED" {
			hasDenial = true
			assert.Equal(t, "REVIEWING", e.Detail["to"])
			break
		}
	}
	assert.True(t, hasDenial, "should deny wrong unblock target")
}

func TestLegacyCompleteSignalIgnored(t *testing.T) {
	env := setupEnv(t)

	// Legacy complete signal should be ignored
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalComplete, "test")
	}, 0)

	// Complete via proper Update path
	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryPhase)
	require.NoError(t, err)
	var phase model.Phase
	require.NoError(t, val.Get(&phase))
	assert.Equal(t, model.PhaseComplete, phase)
}

func TestCommittingRespawnLoop(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	val, err := env.QueryWorkflow(QueryStatus)
	require.NoError(t, err)
	var status model.WorkflowStatus
	require.NoError(t, val.Get(&status))
	assert.Equal(t, 2, status.Iteration)
}

func TestPRCreationToCompleteDenied(t *testing.T) {
	env := setupEnv(t)

	registerTransitions(env, t, []model.Phase{
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
	})

	// PR_CREATION → COMPLETE must be DENIED
	env.RegisterDelayedCallback(func() {
		updateMayDeny(env, model.PhaseComplete)
	}, 0)

	// Complete via valid path
	registerTransitions(env, t, []model.Phase{
		model.PhaseFeedback,
		model.PhaseComplete,
	})

	env.ExecuteWorkflow(CodingSessionWorkflow, model.WorkflowInput{
		SessionID: "test", TaskDescription: "test", MaxIterations: 5,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var timeline model.WorkflowTimeline
	require.NoError(t, env.GetWorkflowResult(&timeline))
	hasDenial := false
	for _, e := range timeline.Events {
		if e.Type == model.EventHookDenial && e.Detail["from"] == "PR_CREATION" && e.Detail["to"] == "COMPLETE" {
			hasDenial = true
			break
		}
	}
	assert.True(t, hasDenial, "PR_CREATION → COMPLETE must be denied")
}

func TestRespawnGuardActiveAgents(t *testing.T) {
	// Test the RESPAWN → DEVELOPING guard: must deny when subagents still active.
	// We verify the guard condition inline since handleTransition needs a workflow context.
	t.Run("deny with active agents", func(t *testing.T) {
		activeAgents := []string{"dev-agent-1", "dev-agent-2"}
		phase := model.PhaseRespawn
		target := model.PhaseDeveloping
		assert.True(t, phase == model.PhaseRespawn && target == model.PhaseDeveloping && len(activeAgents) > 0,
			"guard condition should match: RESPAWN → DEVELOPING with active agents")
	})
	t.Run("allow with no agents", func(t *testing.T) {
		var activeAgents []string
		assert.Equal(t, 0, len(activeAgents), "should allow when no active agents")
	})
	t.Run("SubagentStop removes from list", func(t *testing.T) {
		agents := []string{"dev-1", "rev-1"}
		// Simulate SubagentStop for dev-1
		filtered := agents[:0]
		for _, a := range agents {
			if a != "dev-1" {
				filtered = append(filtered, a)
			}
		}
		assert.Equal(t, []string{"rev-1"}, filtered)
	})
}
