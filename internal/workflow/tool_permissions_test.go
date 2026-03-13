package workflow

import (
	"encoding/json"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- IsSubagent tests ---

func TestIsSubagent_EmptyAgentID(t *testing.T) {
	assert.False(t, IsSubagent("", []string{"agent-1", "agent-2"}),
		"empty agent_id should never be treated as subagent (it is Team Lead)")
}

func TestIsSubagent_AgentInList(t *testing.T) {
	assert.True(t, IsSubagent("agent-abc", []string{"agent-abc", "agent-xyz"}))
}

func TestIsSubagent_AgentNotInList(t *testing.T) {
	assert.False(t, IsSubagent("agent-unknown", []string{"agent-abc", "agent-xyz"}))
}

func TestIsSubagent_EmptyList(t *testing.T) {
	assert.False(t, IsSubagent("agent-abc", []string{}))
}

func TestIsSubagent_NilList(t *testing.T) {
	assert.False(t, IsSubagent("agent-abc", nil))
}

// teamLeadArgs returns agentID and activeAgents for a Team Lead caller (main agent, not a subagent).
func teamLeadArgs() (string, []string) {
	// Empty agentID means the caller is not in any active-agents list → treated as Team Lead.
	return "", nil
}

// subagentArgs returns agentID and activeAgents for a subagent caller.
func subagentArgs() (string, []string) {
	return "agent-123", []string{"agent-123"}
}

// --- CheckToolPermission Team Lead write guard tests ---

func TestCheckToolPermission_TeamLeadDeniedEdit(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedWrite(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedNotebookEdit(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "NotebookEdit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadAllowedRead(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Read", nil, agentID, activeAgents)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadAllowedBash(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadDeniedEditAllPhases(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	phases := []model.Phase{
		model.PhaseDeveloping, model.PhaseReviewing, model.PhaseCommitting,
		model.PhasePRCreation, model.PhaseFeedback, model.PhaseBlocked, model.PhaseComplete,
	}
	for _, phase := range phases {
		result := CheckToolPermission(phase, "Edit", nil, agentID, activeAgents)
		assert.True(t, result.Denied, "Team Lead Edit should be denied in phase %s", phase)
	}
}

func TestCheckToolPermission_SubagentAllowedEditInDeveloping(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (subagent) should be allowed to Edit in DEVELOPING")
}

func TestCheckToolPermission_SubagentAllowedWriteInDeveloping(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (subagent) should be allowed to Write in DEVELOPING")
}

func TestCheckToolPermission_ExistingRespawnWriteDenied(t *testing.T) {
	// Existing rule: RESPAWN denies writes regardless of role
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhaseRespawn, "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Edit should be denied in RESPAWN even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied(t *testing.T) {
	// Existing rule: PLANNING denies writes
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhasePlanning, "Write", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Write should be denied in PLANNING even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied_TeamLeadAlsoDenied(t *testing.T) {
	// Team Lead guard fires first, but result is same
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.PhasePlanning, "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	// The denial reason should mention Team Lead (team-lead guard fires first)
	assert.Contains(t, result.Reason, "Team Lead")
}

// --- Regression: existing git blocking still works ---

func TestCheckToolPermission_GitCommitDeniedOutsideCommitting(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied)
}

func TestCheckToolPermission_GitCommitAllowedInCommitting(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseCommitting, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied)
}

// --- Item 5: checkBashPermission fail-closed tests ---

func TestCheckToolPermission_BashEmptyInputDenied(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	// nil input → unmarshal fails → fail-closed
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "cannot parse Bash command input")
}

func TestCheckToolPermission_BashEmptyCommandDenied(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	input, _ := json.Marshal(map[string]string{"command": ""})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "cannot parse Bash command input")
}

// --- Item 2: stash list / stash drop tests ---

func TestIsAllowedGitInPlanning_StashList(t *testing.T) {
	assert.True(t, isAllowedGitInPlanning("git stash list"), "git stash list is read-only and should be allowed")
}

func TestIsAllowedGitInPlanning_StashShow(t *testing.T) {
	assert.True(t, isAllowedGitInPlanning("git stash show"), "git stash show is read-only and should be allowed")
}

func TestIsAllowedGitInPlanning_StashDrop(t *testing.T) {
	assert.False(t, isAllowedGitInPlanning("git stash drop"), "git stash drop modifies state and should be denied")
}

func TestIsAllowedGitInPlanning_StashPop(t *testing.T) {
	assert.False(t, isAllowedGitInPlanning("git stash pop"), "git stash pop modifies state and should be denied")
}

func TestIsAllowedGitInPlanning_BareStash(t *testing.T) {
	assert.False(t, isAllowedGitInPlanning("git stash"), "bare git stash saves changes and should be denied")
}
