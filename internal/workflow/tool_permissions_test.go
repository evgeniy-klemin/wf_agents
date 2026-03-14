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

// --- Auto-allow (Allowed field) tests ---

func TestCheckToolPermission_ReadOnlyToolsAutoAllowed(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	readOnlyToolNames := []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch", "ToolSearch", "LSP"}
	for _, tool := range readOnlyToolNames {
		result := CheckToolPermission(model.PhaseDeveloping, tool, nil, agentID, activeAgents)
		assert.False(t, result.Denied, "read-only tool %s should not be denied", tool)
		assert.True(t, result.Allowed, "read-only tool %s should be auto-allowed (Allowed: true)", tool)
	}
}

func TestCheckToolPermission_SafeBashAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	safeCmds := []string{
		"go test ./...",
		"go vet ./...",
		"go build ./...",
		"git diff",
		"git diff --stat",
		"git status",
	}
	for _, cmd := range safeCmds {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
		assert.False(t, result.Denied, "safe bash command %q should not be denied", cmd)
		assert.True(t, result.Allowed, "safe bash command %q should be auto-allowed", cmd)
	}
}

func TestCheckToolPermission_UnsafeBashNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	// rm -rf / is not in the safe list, but in DEVELOPING it's not denied (only git is blocked)
	// It should NOT be auto-allowed
	input, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "rm -rf / is not denied in DEVELOPING (only git commands are blocked)")
	assert.False(t, result.Allowed, "rm -rf / should NOT be auto-allowed")
}

func TestCheckToolPermission_DeniedNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	// git commit in DEVELOPING is denied — must NOT be auto-allowed
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "git commit should be denied in DEVELOPING")
	assert.False(t, result.Allowed, "denied command should not be auto-allowed")
}

func TestCheckToolPermission_WfClientAutoAllowedInPlanning(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	// wf-client (by basename extraction) should always be allowed in PLANNING
	input, _ := json.Marshal(map[string]string{"command": "/some/path/bin/wf-client transition wf-id --to RESPAWN --reason \"test\""})
	result := CheckToolPermission(model.PhasePlanning, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "wf-client command should not be denied in PLANNING")
}

func TestCheckToolPermission_WfClientShortNameAllowedInPlanning(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "wf-client status wf-id"})
	result := CheckToolPermission(model.PhasePlanning, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "wf-client (short name) should not be denied in PLANNING")
}

// --- Subagent auto-allow for file-writing tools ---

func TestCheckToolPermission_SubagentEditAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "subagent Edit should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "subagent Edit should be auto-allowed (Allowed: true) in DEVELOPING")
}

func TestCheckToolPermission_SubagentWriteAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "subagent Write should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "subagent Write should be auto-allowed (Allowed: true) in DEVELOPING")
}

func TestCheckToolPermission_TeamLeadBashNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	// "node server.js" is not in the safe prefix list — should not be auto-approved for Team Lead
	input, _ := json.Marshal(map[string]string{"command": "node server.js"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead Bash with node server.js is not denied in DEVELOPING")
	assert.False(t, result.Allowed, "Team Lead Bash with node server.js should NOT be auto-allowed")
}

// --- Auto-approve narrow list tests ---

func TestCheckToolPermission_CurlNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	// curl is in safeBashPrefixes (PLANNING whitelist) but NOT in autoApproveBashPrefixes
	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "curl is not denied in DEVELOPING (only git commands are blocked)")
	assert.False(t, result.Allowed, "curl should NOT be auto-approved (not in narrow auto-approve list)")
}

func TestCheckToolPermission_GitDiffAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	input, _ := json.Marshal(map[string]string{"command": "git diff"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git diff should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "git diff should be auto-approved (truly read-only)")
}

func TestCheckToolPermission_GitConfigNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := subagentArgs()
	// git config can write — must not be auto-approved
	input, _ := json.Marshal(map[string]string{"command": "git config user.name"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git config is not denied in DEVELOPING (not a forbidden git command)")
	assert.False(t, result.Allowed, "git config should NOT be auto-approved (can write)")
}

// --- isSafeBashCommand path-stripping tests ---

func TestIsSafeBashCommand_WithPath(t *testing.T) {
	// /usr/bin/ls -la should match "ls" prefix via basename extraction
	assert.True(t, isSafeBashCommand("/usr/bin/ls -la"),
		"/usr/bin/ls -la should match safe prefix 'ls' via basename extraction")
}

func TestIsSafeBashCommand_WithAbsolutePathWfClient(t *testing.T) {
	// /path/to/bin/wf-client status foo should match "wf-client" prefix via basename extraction
	assert.True(t, isSafeBashCommand("/path/to/bin/wf-client status foo"),
		"/path/to/bin/wf-client status foo should match safe prefix 'wf-client' via basename extraction")
}
