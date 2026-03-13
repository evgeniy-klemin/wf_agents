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

// --- CheckToolPermission Team Lead write guard tests ---

func TestCheckToolPermission_TeamLeadDeniedEdit(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, true)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedWrite(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, true)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedNotebookEdit(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "NotebookEdit", nil, true)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadAllowedRead(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "Read", nil, true)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadAllowedBash(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, true)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadDeniedEditAllPhases(t *testing.T) {
	phases := []model.Phase{
		model.PhaseDeveloping, model.PhaseReviewing, model.PhaseCommitting,
		model.PhasePRCreation, model.PhaseFeedback, model.PhaseBlocked, model.PhaseComplete,
	}
	for _, phase := range phases {
		result := CheckToolPermission(phase, "Edit", nil, true)
		assert.True(t, result.Denied, "Team Lead Edit should be denied in phase %s", phase)
	}
}

func TestCheckToolPermission_SubagentAllowedEditInDeveloping(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, false)
	assert.False(t, result.Denied, "Developer (subagent) should be allowed to Edit in DEVELOPING")
}

func TestCheckToolPermission_SubagentAllowedWriteInDeveloping(t *testing.T) {
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, false)
	assert.False(t, result.Denied, "Developer (subagent) should be allowed to Write in DEVELOPING")
}

func TestCheckToolPermission_ExistingRespawnWriteDenied(t *testing.T) {
	// Existing rule: RESPAWN denies writes regardless of isTeamLead
	result := CheckToolPermission(model.PhaseRespawn, "Edit", nil, false)
	assert.True(t, result.Denied, "Edit should be denied in RESPAWN even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied(t *testing.T) {
	// Existing rule: PLANNING denies writes
	result := CheckToolPermission(model.PhasePlanning, "Write", nil, false)
	assert.True(t, result.Denied, "Write should be denied in PLANNING even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied_TeamLeadAlsoDenied(t *testing.T) {
	// Team Lead guard fires first, but result is same
	result := CheckToolPermission(model.PhasePlanning, "Edit", nil, true)
	assert.True(t, result.Denied)
	// The denial reason should mention Team Lead (team-lead guard fires first)
	assert.Contains(t, result.Reason, "Team Lead")
}

// --- Regression: existing git blocking still works ---

func TestCheckToolPermission_GitCommitDeniedOutsideCommitting(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, false)
	assert.True(t, result.Denied)
}

func TestCheckToolPermission_GitCommitAllowedInCommitting(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseCommitting, "Bash", input, false)
	assert.False(t, result.Denied)
}
