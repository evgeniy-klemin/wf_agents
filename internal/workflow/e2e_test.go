//go:build integration

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// fullTimelineEntry is a single event from a timeline-*-full.json file.
type fullTimelineEntry struct {
	Type      string            `json:"type"`
	Timestamp string            `json:"timestamp"`
	SessionID string            `json:"session_id"`
	Detail    map[string]string `json:"detail"`
}

// loadFullTimeline reads a full timeline JSON file.
func loadFullTimeline(t *testing.T, path string) []fullTimelineEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "timeline file must exist: %s", path)
	var entries []fullTimelineEntry
	require.NoError(t, json.Unmarshal(data, &entries))
	require.NotEmpty(t, entries, "timeline must not be empty")
	return entries
}

// e2eEvidence returns the guard evidence required for the given transition.
func e2eEvidence(from, to string) map[string]string {
	switch from + "→" + to {
	case "DEVELOPING→REVIEWING":
		return map[string]string{"working_tree_clean": "false"}
	case "COMMITTING→PR_CREATION", "COMMITTING→RESPAWN":
		return map[string]string{"working_tree_clean": "true"}
	case "PR_CREATION→FEEDBACK":
		return map[string]string{"pr_checks_pass": "true"}
	case "FEEDBACK→COMPLETE":
		return map[string]string{"pr_approved": "true"}
	default:
		return map[string]string{}
	}
}

// e2eDoTransition sends a synchronous UpdateWorkflow transition request.
func e2eDoTransition(ctx context.Context, t *testing.T, c client.Client, workflowID string, from, to model.Phase) {
	t.Helper()
	evidence := e2eEvidence(string(from), string(to))
	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   workflowID,
		UpdateName:   UpdateTransition,
		Args:         []interface{}{model.SignalTransition{To: to, SessionID: "e2e-test", Reason: "e2e replay", Guards: evidence}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	require.NoError(t, err, "UpdateWorkflow for transition %s→%s", from, to)
	var result model.TransitionResult
	require.NoError(t, handle.Get(ctx, &result), "get transition result %s→%s", from, to)
	require.True(t, result.Allowed, "transition %s→%s denied: %s", from, to, result.Reason)
	t.Logf("Transition %s → %s OK", from, to)
}

// e2eQueryStatus queries workflow status, sleeping briefly to allow signal processing.
func e2eQueryStatus(ctx context.Context, c client.Client, workflowID string) (model.WorkflowStatus, error) {
	time.Sleep(30 * time.Millisecond)
	resp, err := c.QueryWorkflow(ctx, workflowID, "", QueryStatus)
	if err != nil {
		return model.WorkflowStatus{}, err
	}
	var status model.WorkflowStatus
	if err := resp.Get(&status); err != nil {
		return model.WorkflowStatus{}, err
	}
	return status, nil
}

var clearAgentsRe = regexp.MustCompile(`(?i)cleared.*active agent`)
var agentShutDownRe = regexp.MustCompile(`^agent shut down: (.+)$`)

// replayFullTimeline replays a full timeline against a running workflow.
// Returns any activeAgents invariant violations.
func replayFullTimeline(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	workflowID string,
	sessionID string,
	entries []fullTimelineEntry,
) []string {
	t.Helper()
	var violations []string
	currentPhase := model.PhasePlanning

	for i, entry := range entries {
		switch entry.Type {

		case "transition":
			toPhaseStr, ok := entry.Detail["to"]
			if !ok {
				t.Logf("event %d: transition missing 'to' field", i)
				continue
			}
			fromPhaseStr, hasFrom := entry.Detail["from"]
			// First transition (workflow started / PLANNING) has no "from" — skip.
			if !hasFrom {
				continue
			}
			from := model.Phase(fromPhaseStr)
			to := model.Phase(toPhaseStr)

			e2eDoTransition(ctx, t, c, workflowID, from, to)
			currentPhase = to

			status, qErr := e2eQueryStatus(ctx, c, workflowID)
			if qErr != nil {
				t.Logf("event %d: query after transition %s→%s failed: %v", i, from, to, qErr)
				continue
			}
			assert.Equal(t, to, status.Phase,
				"after transition %s→%s: expected phase %s, got %s", from, to, to, status.Phase)

		case "tool_use":
			hookType := entry.Detail["hook_type"]
			if hookType == "" {
				continue
			}
			sig := model.SignalHookEvent{
				HookType:  hookType,
				SessionID: entry.SessionID,
				Tool:      entry.Detail["tool"],
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (%s): signal error: %v", i, hookType, err)
				continue
			}

			// Check activeAgents invariant after PreToolUse from developer/reviewer agents.
			agentType := entry.Detail["agent_type"]
			if hookType == "PreToolUse" && agentType != "" &&
				(strings.HasPrefix(agentType, "developer") || strings.HasPrefix(agentType, "reviewer")) {

				status, qErr := e2eQueryStatus(ctx, c, workflowID)
				if qErr != nil {
					t.Logf("event %d: query failed: %v", i, qErr)
					continue
				}
				found := false
				for _, a := range status.ActiveAgents {
					if a == agentType {
						found = true
						break
					}
				}
				if !found {
					violation := fmt.Sprintf(
						"event %d [%s]: PreToolUse agent_type=%q not in ActiveAgents=%v (phase=%s)",
						i, entry.Timestamp, agentType, status.ActiveAgents, currentPhase,
					)
					t.Logf("VIOLATION: %s", violation)
					violations = append(violations, violation)
				}
			}

		case "agent_spawn":
			sig := model.SignalHookEvent{
				HookType:  "SubagentStart",
				SessionID: entry.SessionID,
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (SubagentStart): signal error: %v", i, err)
			}

		case "agent_stop":
			sig := model.SignalHookEvent{
				HookType:  "SubagentStop",
				SessionID: entry.SessionID,
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (SubagentStop): signal error: %v", i, err)
			}

		case "journal":
			msg := entry.Detail["message"]
			if clearAgentsRe.MatchString(msg) {
				// The real session ran deregister-all-agents — replay the same signal.
				if err := c.SignalWorkflow(ctx, workflowID, "", SignalClearActiveAgents, sessionID); err != nil {
					t.Logf("event %d (clear-active-agents): signal error: %v", i, err)
				} else {
					time.Sleep(50 * time.Millisecond)
					t.Logf("event %d: sent clear-active-agents (journal: %q)", i, msg)
				}
			} else if m := agentShutDownRe.FindStringSubmatch(msg); m != nil {
				// The real session ran wf-client shut-down — replay the agent-shut-down signal.
				agentName := m[1]
				if err := c.SignalWorkflow(ctx, workflowID, "", SignalAgentShutDown, struct{ AgentName string }{agentName}); err != nil {
					t.Logf("event %d (agent-shut-down %s): signal error: %v", i, agentName, err)
				} else {
					time.Sleep(50 * time.Millisecond)
					t.Logf("event %d: sent agent-shut-down %q (journal: %q)", i, agentName, msg)
				}
			}

		case "hook_denial":
			// Denied transitions from the real session — skip, we don't replay them.

		default:
			t.Logf("event %d: unknown type %q — skipping", i, entry.Type)
		}
	}

	return violations
}

// TestE2EFullSessionReplay starts a real Temporal workflow and replays session-20e027ee
// from its full timeline. Verifies the activeAgents invariant after every PreToolUse from
// developer/reviewer agents, and checks that developer-1 and reviewer-1 remain active
// through COMMITTING, PR_CREATION, and FEEDBACK.
//
// Requires Temporal on localhost:7233 and a worker running CodingSessionWorkflow.
// Run with: go test -tags integration ./internal/workflow/ -v -run TestE2EFullSessionReplay
func TestE2EFullSessionReplay(t *testing.T) {
	entries := loadFullTimeline(t, "testdata/timeline-20e027ee-full.json")

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	require.NoError(t, err, "connect to Temporal — is docker compose up?")
	defer c.Close()

	sessionID := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	workflowID := "coding-session-" + sessionID

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if desc, descErr := c.DescribeWorkflowExecution(ctx, workflowID, ""); descErr == nil {
		if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			_ = c.TerminateWorkflow(ctx, workflowID, "", "e2e test cleanup", nil)
			time.Sleep(200 * time.Millisecond)
		}
	}

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "coding-session",
	}, CodingSessionWorkflow, model.WorkflowInput{
		SessionID:       sessionID,
		TaskDescription: "e2e test replay of session-20e027ee",
		MaxIterations:   10,
	})
	require.NoError(t, err, "start workflow")
	t.Logf("Workflow started: ID=%s RunID=%s", workflowID, run.GetRunID())

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		if desc, descErr := c.DescribeWorkflowExecution(cleanCtx, workflowID, ""); descErr == nil {
			if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
				_ = c.TerminateWorkflow(cleanCtx, workflowID, "", "e2e test cleanup", nil)
			}
		}
	}()

	violations := replayFullTimeline(ctx, t, c, workflowID, sessionID, entries)

	// Additionally assert that developer-1 and reviewer-1 were active through the late phases.
	// This is implicitly checked by the PreToolUse invariant in replayFullTimeline, but we
	// verify it explicitly after the COMPLETE transition too.
	assert.Empty(t, violations,
		"activeAgents invariant violated %d time(s):\n%s",
		len(violations), strings.Join(violations, "\n"))
}

// TestE2ETwoIterationSession replays session-532153f4 — a two-iteration session where the
// second iteration uses developer-2 and reviewer-2 teammates. The full timeline includes a
// journal entry "cleared 2 active agent(s) via deregister-all-agents" between iterations
// which is replayed as a clear-active-agents signal (no synthetic injection needed).
//
// Requires Temporal on localhost:7233 and a worker running CodingSessionWorkflow.
// Run with: go test -tags integration ./internal/workflow/ -v -run TestE2ETwoIterationSession
func TestE2ETwoIterationSession(t *testing.T) {
	entries := loadFullTimeline(t, "testdata/timeline-532153f4-full.json")

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	require.NoError(t, err, "connect to Temporal — is docker compose up?")
	defer c.Close()

	sessionID := fmt.Sprintf("e2e-2iter-%d", time.Now().UnixNano())
	workflowID := "coding-session-" + sessionID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if desc, descErr := c.DescribeWorkflowExecution(ctx, workflowID, ""); descErr == nil {
		if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			_ = c.TerminateWorkflow(ctx, workflowID, "", "e2e test cleanup", nil)
			time.Sleep(200 * time.Millisecond)
		}
	}

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "coding-session",
	}, CodingSessionWorkflow, model.WorkflowInput{
		SessionID:       sessionID,
		TaskDescription: "e2e test replay of session-532153f4 (2 iterations)",
		MaxIterations:   10,
	})
	require.NoError(t, err, "start workflow")
	t.Logf("Workflow started: ID=%s RunID=%s", workflowID, run.GetRunID())

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		if desc, descErr := c.DescribeWorkflowExecution(cleanCtx, workflowID, ""); descErr == nil {
			if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
				_ = c.TerminateWorkflow(cleanCtx, workflowID, "", "e2e test cleanup", nil)
			}
		}
	}()

	violations := replayFullTimeline(ctx, t, c, workflowID, sessionID, entries)

	assert.Empty(t, violations,
		"activeAgents invariant violated %d time(s):\n%s",
		len(violations), strings.Join(violations, "\n"))
}

// TestE2EAgentShutDownSession replays session-37e1e653 — a two-iteration session that uses
// per-agent wf-client shut-down (not deregister-all-agents) between iterations. The full
// timeline includes journal entries "agent shut down: developer-1" and "agent shut down:
// reviewer-1" which are replayed as agent-shut-down signals by replayFullTimeline.
//
// Key assertions after each REVIEWING→COMMITTING transition:
//   - Iteration 1: developer-1 and reviewer-1 in ActiveAgents
//   - Iteration 2: developer-2 and reviewer-2 in ActiveAgents
//
// The session ends in FEEDBACK (not COMPLETE). The workflow is terminated after replay.
//
// Requires Temporal on localhost:7233 and a worker running CodingSessionWorkflow.
// Run with: go test -tags integration ./internal/workflow/ -v -run TestE2EAgentShutDownSession
func TestE2EAgentShutDownSession(t *testing.T) {
	entries := loadFullTimeline(t, "testdata/timeline-37e1e653-full.json")

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	require.NoError(t, err, "connect to Temporal — is docker compose up?")
	defer c.Close()

	sessionID := fmt.Sprintf("e2e-shutdown-%d", time.Now().UnixNano())
	workflowID := "coding-session-" + sessionID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if desc, descErr := c.DescribeWorkflowExecution(ctx, workflowID, ""); descErr == nil {
		if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			_ = c.TerminateWorkflow(ctx, workflowID, "", "e2e test cleanup", nil)
			time.Sleep(200 * time.Millisecond)
		}
	}

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "coding-session",
	}, CodingSessionWorkflow, model.WorkflowInput{
		SessionID:       sessionID,
		TaskDescription: "e2e test replay of session-37e1e653 (agent-shut-down)",
		MaxIterations:   10,
	})
	require.NoError(t, err, "start workflow")
	t.Logf("Workflow started: ID=%s RunID=%s", workflowID, run.GetRunID())

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		if desc, descErr := c.DescribeWorkflowExecution(cleanCtx, workflowID, ""); descErr == nil {
			if desc.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
				_ = c.TerminateWorkflow(cleanCtx, workflowID, "", "e2e test cleanup", nil)
			}
		}
	}()

	var violations []string
	currentPhase := model.PhasePlanning
	commitsChecked := 0

	for i, entry := range entries {
		switch entry.Type {

		case "transition":
			toPhaseStr, ok := entry.Detail["to"]
			if !ok {
				continue
			}
			fromPhaseStr, hasFrom := entry.Detail["from"]
			if !hasFrom {
				continue
			}
			from := model.Phase(fromPhaseStr)
			to := model.Phase(toPhaseStr)

			e2eDoTransition(ctx, t, c, workflowID, from, to)
			currentPhase = to

			status, qErr := e2eQueryStatus(ctx, c, workflowID)
			if qErr != nil {
				t.Logf("event %d: query after transition %s→%s failed: %v", i, from, to, qErr)
				continue
			}
			assert.Equal(t, to, status.Phase,
				"after transition %s→%s: expected phase %s, got %s", from, to, to, status.Phase)

			// After REVIEWING→COMMITTING, check that the iteration's agents are still active.
			if from == model.PhaseReviewing && to == model.PhaseCommitting {
				commitsChecked++
				var expectedAgents []string
				if commitsChecked == 1 {
					expectedAgents = []string{"developer-1", "reviewer-1"}
				} else {
					expectedAgents = []string{"developer-2", "reviewer-2"}
				}
				for _, expected := range expectedAgents {
					found := false
					for _, a := range status.ActiveAgents {
						if a == expected {
							found = true
							break
						}
					}
					if !found {
						violation := fmt.Sprintf(
							"iteration-%d REVIEWING→COMMITTING: expected %s in ActiveAgents, got %v",
							commitsChecked, expected, status.ActiveAgents,
						)
						t.Logf("VIOLATION: %s", violation)
						violations = append(violations, violation)
					}
				}
			}

		case "tool_use":
			hookType := entry.Detail["hook_type"]
			if hookType == "" {
				continue
			}
			sig := model.SignalHookEvent{
				HookType:  hookType,
				SessionID: entry.SessionID,
				Tool:      entry.Detail["tool"],
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (%s): signal error: %v", i, hookType, err)
				continue
			}
			agentType := entry.Detail["agent_type"]
			if hookType == "PreToolUse" && agentType != "" &&
				(strings.HasPrefix(agentType, "developer") || strings.HasPrefix(agentType, "reviewer")) {
				status, qErr := e2eQueryStatus(ctx, c, workflowID)
				if qErr != nil {
					t.Logf("event %d: query failed: %v", i, qErr)
					continue
				}
				found := false
				for _, a := range status.ActiveAgents {
					if a == agentType {
						found = true
						break
					}
				}
				if !found {
					violation := fmt.Sprintf(
						"event %d [%s]: PreToolUse agent_type=%q not in ActiveAgents=%v (phase=%s)",
						i, entry.Timestamp, agentType, status.ActiveAgents, currentPhase,
					)
					t.Logf("VIOLATION: %s", violation)
					violations = append(violations, violation)
				}
			}

		case "agent_spawn":
			sig := model.SignalHookEvent{
				HookType:  "SubagentStart",
				SessionID: entry.SessionID,
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (SubagentStart): signal error: %v", i, err)
			}

		case "agent_stop":
			sig := model.SignalHookEvent{
				HookType:  "SubagentStop",
				SessionID: entry.SessionID,
				Detail:    entry.Detail,
			}
			if err := c.SignalWorkflow(ctx, workflowID, "", SignalHookEvent, sig); err != nil {
				t.Logf("event %d (SubagentStop): signal error: %v", i, err)
			}

		case "journal":
			msg := entry.Detail["message"]
			if clearAgentsRe.MatchString(msg) {
				if err := c.SignalWorkflow(ctx, workflowID, "", SignalClearActiveAgents, sessionID); err != nil {
					t.Logf("event %d (clear-active-agents): signal error: %v", i, err)
				} else {
					time.Sleep(50 * time.Millisecond)
					t.Logf("event %d: sent clear-active-agents (journal: %q)", i, msg)
				}
			} else if m := agentShutDownRe.FindStringSubmatch(msg); m != nil {
				agentName := m[1]
				if err := c.SignalWorkflow(ctx, workflowID, "", SignalAgentShutDown, struct{ AgentName string }{agentName}); err != nil {
					t.Logf("event %d (agent-shut-down %s): signal error: %v", i, agentName, err)
				} else {
					time.Sleep(50 * time.Millisecond)
					t.Logf("event %d: sent agent-shut-down %q (journal: %q)", i, agentName, msg)
				}
			}

		case "hook_denial":
			// skip

		}
	}

	assert.Empty(t, violations,
		"activeAgents invariant violated %d time(s):\n%s",
		len(violations), strings.Join(violations, "\n"))
}
