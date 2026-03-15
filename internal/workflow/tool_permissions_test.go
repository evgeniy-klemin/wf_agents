package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- IsTeammate tests ---

func TestIsTeammate_EmptyAgentID(t *testing.T) {
	assert.False(t, IsTeammate("", []string{"agent-1", "agent-2"}),
		"empty agent_id should never be treated as teammate (it is Team Lead)")
}

// IsTeammate now returns true for any non-empty agentID (Agent Teams teammates
// don't always appear in activeAgents before their first PreToolUse fires).
func TestIsTeammate_AgentInList(t *testing.T) {
	assert.True(t, IsTeammate("agent-abc", []string{"agent-abc", "agent-xyz"}))
}

func TestIsTeammate_AgentNotInList(t *testing.T) {
	// After simplification: any non-empty agentID is treated as a teammate,
	// even if not yet in the activeAgents list (will be auto-registered).
	assert.True(t, IsTeammate("agent-unknown", []string{"agent-abc", "agent-xyz"}))
}

func TestIsTeammate_EmptyList(t *testing.T) {
	// Any non-empty agentID → teammate, even if list is empty.
	assert.True(t, IsTeammate("agent-abc", []string{}))
}

func TestIsTeammate_NilList(t *testing.T) {
	// Any non-empty agentID → teammate, even if list is nil.
	assert.True(t, IsTeammate("agent-abc", nil))
}

// teamLeadArgs returns agentID and activeAgents for a Team Lead caller (main agent, not a teammate).
func teamLeadArgs() (string, []string) {
	// Empty agentID means the caller is not in any active-agents list → treated as Team Lead.
	return "", nil
}

// teammateArgs returns agentID and activeAgents for a teammate caller.
func teammateArgs() (string, []string) {
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

func TestCheckToolPermission_TeammateAllowedEditInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (teammate) should be allowed to Edit in DEVELOPING")
}

func TestCheckToolPermission_TeammateAllowedWriteInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (teammate) should be allowed to Write in DEVELOPING")
}

func TestCheckToolPermission_ExistingRespawnWriteDenied(t *testing.T) {
	// Existing rule: RESPAWN denies writes regardless of role
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhaseRespawn, "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Edit should be denied in RESPAWN even for teammate")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied(t *testing.T) {
	// Existing rule: PLANNING denies writes
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhasePlanning, "Write", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Write should be denied in PLANNING even for teammate")
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
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied)
}

func TestCheckToolPermission_GitCommitAllowedInCommitting(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.PhaseCommitting, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied)
}

// --- Item 5: checkBashPermission fail-closed tests ---

func TestCheckToolPermission_BashEmptyInputDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// nil input → unmarshal fails → fail-closed
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "cannot parse Bash command input")
}

func TestCheckToolPermission_BashEmptyCommandDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
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
	agentID, activeAgents := teammateArgs()
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

func TestCheckToolPermission_UnsafeBashAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// rm -rf / is not in the safe list, but in DEVELOPING it's not denied (only git is blocked).
	// Teammates get auto-approved for any non-denied Bash command (permission bypass).
	input, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "rm -rf / is not denied in DEVELOPING (only git commands are blocked)")
	assert.True(t, result.Allowed, "rm -rf / should be auto-approved for teammates (permission bypass)")
}

func TestCheckToolPermission_DeniedNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := teammateArgs()
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

// --- Teammate auto-allow for file-writing tools ---

func TestCheckToolPermission_TeammateEditAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "teammate Edit should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "teammate Edit should be auto-allowed (Allowed: true) in DEVELOPING")
}

func TestCheckToolPermission_TeammateWriteAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.PhaseDeveloping, "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "teammate Write should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "teammate Write should be auto-allowed (Allowed: true) in DEVELOPING")
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

func TestCheckToolPermission_CurlAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// curl is in safeBashPrefixes (PLANNING whitelist) but NOT in autoApproveBashPrefixes.
	// However, teammates get auto-approved for any non-denied Bash command (same as non-Bash tools).
	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "curl is not denied in DEVELOPING (only git commands are blocked)")
	assert.True(t, result.Allowed, "curl should be auto-approved for teammates (bypass permission prompt)")
}

func TestCheckToolPermission_GitDiffAutoAllowed(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git diff"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git diff should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "git diff should be auto-approved (truly read-only)")
}

func TestCheckToolPermission_GitConfigAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// git config can write but is not in the forbidden list, so it's not denied.
	// Teammates get auto-approved for any non-denied Bash command.
	input, _ := json.Marshal(map[string]string{"command": "git config user.name"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git config is not denied in DEVELOPING (not a forbidden git command)")
	assert.True(t, result.Allowed, "git config should be auto-approved for teammates (bypass permission prompt)")
}

// --- isClaudeInfraFile tests ---

func TestIsClaudeInfraFile_PlansFile(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/plans/foo.md"})
	assert.True(t, isClaudeInfraFile(input), "/.claude/plans/ path should be recognized as Claude infra file")
}

func TestIsClaudeInfraFile_MemoryFile(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/projects/proj123/memory/bar.md"})
	assert.True(t, isClaudeInfraFile(input), "/.claude/projects/.../memory/ path should be recognized as Claude infra file")
}

func TestIsClaudeInfraFile_ProjectFile(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/cmd/client/main.go"})
	assert.False(t, isClaudeInfraFile(input), "regular project file should NOT be recognized as Claude infra file")
}

func TestIsClaudeInfraFile_InvalidJSON(t *testing.T) {
	assert.False(t, isClaudeInfraFile([]byte("not json")), "invalid JSON should return false (fail-closed)")
}

func TestIsClaudeInfraFile_NilInput(t *testing.T) {
	assert.False(t, isClaudeInfraFile(nil), "nil input should return false (fail-closed)")
}

// --- Team Lead can write Claude infra files (plan/memory) ---

func TestCheckToolPermission_TeamLeadAllowedWriteToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/plans/iteration-plan.md"})
	result := CheckToolPermission(model.PhaseDeveloping, "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Write to /.claude/plans/ files")
}

func TestCheckToolPermission_TeamLeadAllowedEditToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/home/user/.claude/plans/my-plan.md"})
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Edit /.claude/plans/ files")
}

func TestCheckToolPermission_TeamLeadAllowedWriteToMemoryFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/projects/abc/memory/notes.md"})
	result := CheckToolPermission(model.PhaseDeveloping, "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Write to /.claude/projects/.../memory/ files")
}

func TestCheckToolPermission_TeamLeadStillDeniedEditToProjectFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/cmd/client/main.go"})
	result := CheckToolPermission(model.PhaseDeveloping, "Edit", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead should still be denied from editing regular project files")
	assert.Contains(t, result.Reason, "Team Lead")
}

// --- PLANNING phase: Claude infra files allowed, project files still denied ---

func TestCheckToolPermission_PlanningAllowedWriteToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/plans/my-plan.md"})
	result := CheckToolPermission(model.PhasePlanning, "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Write to /.claude/plans/ should be allowed in PLANNING")
}

func TestCheckToolPermission_PlanningAllowedWriteToMemoryFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/projects/proj/memory/mem.md"})
	result := CheckToolPermission(model.PhasePlanning, "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Write to /.claude/projects/.../memory/ should be allowed in PLANNING")
}

func TestCheckToolPermission_PlanningStillDeniedWriteToProjectFile(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/cmd/client/main.go"})
	result := CheckToolPermission(model.PhasePlanning, "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Write to project file should still be denied in PLANNING")
}

func TestCheckToolPermission_RespawnStillDeniedWriteToProjectFile(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/internal/workflow/guards.go"})
	result := CheckToolPermission(model.PhaseRespawn, "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Write to project file should still be denied in RESPAWN")
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

// --- splitBashCommands: && splitting and 2>&1 non-split ---

func TestSplitBashCommands_DoubleAmpersand(t *testing.T) {
	// "cmd1 && cmd2 && cmd3" should split into exactly 3 segments
	parts := splitBashCommands("cmd1 && cmd2 && cmd3")
	var nonempty []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			nonempty = append(nonempty, s)
		}
	}
	assert.Equal(t, 3, len(nonempty), "cmd1 && cmd2 && cmd3 should split into 3 segments, got: %v", nonempty)
	assert.Equal(t, "cmd1", nonempty[0])
	assert.Equal(t, "cmd2", nonempty[1])
	assert.Equal(t, "cmd3", nonempty[2])
}

func TestSplitBashCommands_RedirectionNotSplit(t *testing.T) {
	// "git log --oneline 2>&1" should NOT split at &1 — only && should split
	parts := splitBashCommands("git log --oneline 2>&1")
	var nonempty []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			nonempty = append(nonempty, s)
		}
	}
	assert.Equal(t, 1, len(nonempty), "2>&1 should NOT cause a split, got: %v", nonempty)
}

// --- && splitting security: chained forbidden git commands are caught ---

func TestCheckToolPermission_AndAndGitLogAutoApproved(t *testing.T) {
	// "pwd && git log --oneline" in REVIEWING should be auto-approved (both segments safe)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "pwd && git log --oneline"})
	result := CheckToolPermission(model.PhaseReviewing, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "pwd && git log --oneline should not be denied in REVIEWING")
	assert.True(t, result.Allowed, "pwd && git log --oneline should be auto-approved in REVIEWING")
}

func TestCheckToolPermission_AndAndGitCommitDenied(t *testing.T) {
	// "pwd && git commit -m 'x'" in REVIEWING should be denied (git commit forbidden)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "pwd && git commit -m \"x\""})
	result := CheckToolPermission(model.PhaseReviewing, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "pwd && git commit -m 'x' should be denied in REVIEWING — git commit not allowed")
}

func TestCheckToolPermission_GitLogRedirectionAutoApproved(t *testing.T) {
	// "git log --oneline 2>&1" in REVIEWING should be auto-approved (no split at &1)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git log --oneline 2>&1"})
	result := CheckToolPermission(model.PhaseReviewing, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git log --oneline 2>&1 should not be denied in REVIEWING")
	assert.True(t, result.Allowed, "git log --oneline 2>&1 should be auto-approved in REVIEWING")
}

func TestCheckToolPermission_EchoAndGitPushDenied(t *testing.T) {
	// "echo hello && git push" in DEVELOPING should be denied (git push caught through &&)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "echo hello && git push"})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "echo hello && git push should be denied in DEVELOPING — git push not allowed")
}

// --- gofmt in autoApproveBashPrefixes ---

func TestCheckToolPermission_GofmtAutoApprovedInDeveloping(t *testing.T) {
	// "gofmt ./..." in DEVELOPING should be auto-approved
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt ./..."})
	result := CheckToolPermission(model.PhaseDeveloping, "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "gofmt ./... should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "gofmt ./... should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_GofmtDeniedInPlanning(t *testing.T) {
	// "gofmt" in PLANNING should be denied — gofmt -w modifies files and is not allowed outside DEVELOPING/REVIEWING
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt -l ./..."})
	result := CheckToolPermission(model.PhasePlanning, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "gofmt should be denied in PLANNING (file-modifying command)")
}

func TestCheckToolPermission_GofmtDeniedInFeedback(t *testing.T) {
	// "gofmt" in FEEDBACK should be denied — only allowed in DEVELOPING/REVIEWING
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt -w ./..."})
	result := CheckToolPermission(model.PhaseFeedback, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "gofmt should be denied in FEEDBACK (file-modifying command)")
	assert.Contains(t, result.Reason, "gofmt is only allowed in DEVELOPING and REVIEWING phases")
}

func TestCheckToolPermission_GofmtDeniedInCommitting(t *testing.T) {
	// "gofmt" in COMMITTING should be denied — only allowed in DEVELOPING/REVIEWING
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt ./..."})
	result := CheckToolPermission(model.PhaseCommitting, "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "gofmt should be denied in COMMITTING (file-modifying command)")
	assert.Contains(t, result.Reason, "gofmt is only allowed in DEVELOPING and REVIEWING phases")
}
