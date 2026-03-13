package main

import (
	"encoding/json"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- isSubagent tests ---

func TestIsSubagent_EmptyAgentID(t *testing.T) {
	assert.False(t, isSubagent("", []string{"agent-1", "agent-2"}),
		"empty agent_id should never be treated as subagent (it is Team Lead)")
}

func TestIsSubagent_AgentInList(t *testing.T) {
	assert.True(t, isSubagent("agent-abc", []string{"agent-abc", "agent-xyz"}))
}

func TestIsSubagent_AgentNotInList(t *testing.T) {
	assert.False(t, isSubagent("agent-unknown", []string{"agent-abc", "agent-xyz"}))
}

func TestIsSubagent_EmptyList(t *testing.T) {
	assert.False(t, isSubagent("agent-abc", []string{}))
}

func TestIsSubagent_NilList(t *testing.T) {
	assert.False(t, isSubagent("agent-abc", nil))
}

// --- checkToolPermission Team Lead write guard tests ---

func TestCheckToolPermission_TeamLeadDeniedEdit(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "Edit", nil, true)
	assert.True(t, result.denied)
	assert.Contains(t, result.reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedWrite(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "Write", nil, true)
	assert.True(t, result.denied)
	assert.Contains(t, result.reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedNotebookEdit(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "NotebookEdit", nil, true)
	assert.True(t, result.denied)
	assert.Contains(t, result.reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadAllowedRead(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "Read", nil, true)
	assert.False(t, result.denied)
}

func TestCheckToolPermission_TeamLeadAllowedBash(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	result := checkToolPermission(model.PhaseDeveloping, "Bash", input, true)
	assert.False(t, result.denied)
}

func TestCheckToolPermission_TeamLeadDeniedEditAllPhases(t *testing.T) {
	phases := []model.Phase{
		model.PhaseDeveloping, model.PhaseReviewing, model.PhaseCommitting,
		model.PhasePRCreation, model.PhaseFeedback, model.PhaseBlocked, model.PhaseComplete,
	}
	for _, phase := range phases {
		result := checkToolPermission(phase, "Edit", nil, true)
		assert.True(t, result.denied, "Team Lead Edit should be denied in phase %s", phase)
	}
}

func TestCheckToolPermission_SubagentAllowedEditInDeveloping(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "Edit", nil, false)
	assert.False(t, result.denied, "Developer (subagent) should be allowed to Edit in DEVELOPING")
}

func TestCheckToolPermission_SubagentAllowedWriteInDeveloping(t *testing.T) {
	result := checkToolPermission(model.PhaseDeveloping, "Write", nil, false)
	assert.False(t, result.denied, "Developer (subagent) should be allowed to Write in DEVELOPING")
}

func TestCheckToolPermission_ExistingRespawnWriteDenied(t *testing.T) {
	// Existing rule: RESPAWN denies writes regardless of isTeamLead
	result := checkToolPermission(model.PhaseRespawn, "Edit", nil, false)
	assert.True(t, result.denied, "Edit should be denied in RESPAWN even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied(t *testing.T) {
	// Existing rule: PLANNING denies writes
	result := checkToolPermission(model.PhasePlanning, "Write", nil, false)
	assert.True(t, result.denied, "Write should be denied in PLANNING even for subagent")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied_TeamLeadAlsoDenied(t *testing.T) {
	// Team Lead guard fires first, but result is same
	result := checkToolPermission(model.PhasePlanning, "Edit", nil, true)
	assert.True(t, result.denied)
	// The denial reason should mention Team Lead (team-lead guard fires first)
	assert.Contains(t, result.reason, "Team Lead")
}

// --- Regression: existing git blocking still works ---

func TestCheckToolPermission_GitCommitDeniedOutsideCommitting(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := checkToolPermission(model.PhaseDeveloping, "Bash", input, false)
	assert.True(t, result.denied)
}

func TestCheckToolPermission_GitCommitAllowedInCommitting(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := checkToolPermission(model.PhaseCommitting, "Bash", input, false)
	assert.False(t, result.denied)
}
