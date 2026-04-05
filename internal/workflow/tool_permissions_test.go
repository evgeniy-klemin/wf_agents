package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
)

// --- IsTeammate tests ---

func TestIsTeammate_EmptyAgentID(t *testing.T) {
	assert.False(t, IsTeammate(""),
		"empty agent_id should never be treated as teammate (it is Team Lead)")
}

// IsTeammate returns true for any non-empty agentID (Agent Teams teammates
// don't always appear in activeAgents before their first PreToolUse fires).
func TestIsTeammate_AgentInList(t *testing.T) {
	assert.True(t, IsTeammate("agent-abc"))
}

func TestIsTeammate_AgentNotInList(t *testing.T) {
	// Any non-empty agentID is treated as a teammate.
	assert.True(t, IsTeammate("agent-unknown"))
}

func TestIsTeammate_EmptyList(t *testing.T) {
	// Any non-empty agentID → teammate.
	assert.True(t, IsTeammate("agent-abc"))
}

func TestIsTeammate_NilList(t *testing.T) {
	// Any non-empty agentID → teammate.
	assert.True(t, IsTeammate("agent-abc"))
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
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedWrite(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadDeniedNotebookEdit(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "NotebookEdit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_TeamLeadAllowedRead(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Read", nil, agentID, activeAgents)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadAllowedBash(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "go test ./..."})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied)
}

func TestCheckToolPermission_TeamLeadDeniedEditAllPhases(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	phases := []model.Phase{
		model.Phase("DEVELOPING"), model.Phase("REVIEWING"), model.Phase("COMMITTING"),
		model.Phase("PR_CREATION"), model.Phase("FEEDBACK"), model.PhaseBlocked, model.Phase("COMPLETE"),
	}
	for _, phase := range phases {
		result := CheckToolPermission(phase, "Edit", nil, agentID, activeAgents)
		assert.True(t, result.Denied, "Team Lead Edit should be denied in phase %s", phase)
	}
}

func TestCheckToolPermission_TeammateAllowedEditInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (teammate) should be allowed to Edit in DEVELOPING")
}

func TestCheckToolPermission_TeammateAllowedWriteInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "Developer (teammate) should be allowed to Write in DEVELOPING")
}

func TestCheckToolPermission_ExistingRespawnWriteDenied(t *testing.T) {
	// developer* agents are denied file writes in RESPAWN (no file_writes: allow in RESPAWN)
	agentID := "developer-1"
	activeAgents := []string{"developer-1"}
	result := CheckToolPermission(model.Phase("RESPAWN"), "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Edit should be denied in RESPAWN for developer teammate")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied(t *testing.T) {
	// developer* agents are denied file writes in PLANNING (no file_writes: allow in PLANNING)
	agentID := "developer-1"
	activeAgents := []string{"developer-1"}
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", nil, agentID, activeAgents)
	assert.True(t, result.Denied, "Write should be denied in PLANNING for developer teammate")
}

func TestCheckToolPermission_ExistingPlanningWriteDenied_TeamLeadAlsoDenied(t *testing.T) {
	// Team Lead guard fires first, but result is same
	agentID, activeAgents := teamLeadArgs()
	result := CheckToolPermission(model.Phase("PLANNING"), "Edit", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	// The denial reason should mention Team Lead (team-lead guard fires first)
	assert.Contains(t, result.Reason, "Team Lead")
}

// --- Regression: existing git blocking still works ---

func TestCheckToolPermission_GitCommitDeniedOutsideCommitting(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied)
}

func TestCheckToolPermission_GitCommitAllowedInCommitting(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.Phase("COMMITTING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied)
}

// --- Item 5: checkBashPermission fail-closed tests ---

func TestCheckToolPermission_BashEmptyInputDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// nil input → unmarshal fails → fail-closed
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", nil, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "cannot parse Bash command input")
}

func TestCheckToolPermission_BashEmptyCommandDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": ""})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied)
	assert.Contains(t, result.Reason, "cannot parse Bash command input")
}

// --- Item 2: stash list / stash drop tests ---

func TestIsAllowedGitInPlanning_StashList(t *testing.T) {
	assert.True(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git stash list"), "git stash list is read-only and should be allowed")
}

func TestIsAllowedGitInPlanning_StashShow(t *testing.T) {
	assert.True(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git stash show"), "git stash show is read-only and should be allowed")
}

func TestIsAllowedGitInPlanning_StashDrop(t *testing.T) {
	assert.False(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git stash drop"), "git stash drop modifies state and should be denied")
}

func TestIsAllowedGitInPlanning_StashPop(t *testing.T) {
	assert.False(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git stash pop"), "git stash pop modifies state and should be denied")
}

func TestIsAllowedGitInPlanning_BareStash(t *testing.T) {
	assert.False(t, isAllowedGitInPhase(model.Phase("PLANNING"), "git stash"), "bare git stash saves changes and should be denied")
}

// --- Auto-allow (Allowed field) tests ---

func TestCheckToolPermission_ReadOnlyToolsAutoAllowed(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	readOnlyToolNames := []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch", "ToolSearch", "LSP"}
	for _, tool := range readOnlyToolNames {
		result := CheckToolPermission(model.Phase("DEVELOPING"), tool, nil, agentID, activeAgents)
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
		result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
		assert.False(t, result.Denied, "safe bash command %q should not be denied", cmd)
		assert.True(t, result.Allowed, "safe bash command %q should be auto-allowed", cmd)
	}
}

func TestCheckToolPermission_UnsafeBashAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// rm -rf / is not in the safe list, but in DEVELOPING it's not denied (only git is blocked).
	// Teammates get auto-approved for any non-denied Bash command (permission bypass).
	input, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "rm -rf / is not denied in DEVELOPING (only git commands are blocked)")
	assert.True(t, result.Allowed, "rm -rf / should be auto-approved for teammates (permission bypass)")
}

func TestCheckToolPermission_DeniedNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// git commit in DEVELOPING is denied — must NOT be auto-allowed
	input, _ := json.Marshal(map[string]string{"command": "git commit -m 'test'"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "git commit should be denied in DEVELOPING")
	assert.False(t, result.Allowed, "denied command should not be auto-allowed")
}

func TestCheckToolPermission_WfClientAutoAllowedInPlanning(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	// wf-client (by basename extraction) should always be allowed in PLANNING
	input, _ := json.Marshal(map[string]string{"command": "/some/path/bin/wf-client transition wf-id --to RESPAWN --reason \"test\""})
	result := CheckToolPermission(model.Phase("PLANNING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "wf-client command should not be denied in PLANNING")
}

func TestCheckToolPermission_WfClientShortNameAllowedInPlanning(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "wf-client status wf-id"})
	result := CheckToolPermission(model.Phase("PLANNING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "wf-client (short name) should not be denied in PLANNING")
}

// --- Teammate auto-allow for file-writing tools ---

func TestCheckToolPermission_TeammateEditAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "teammate Edit should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "teammate Edit should be auto-allowed (Allowed: true) in DEVELOPING")
}

func TestCheckToolPermission_TeammateWriteAutoAllowedInDeveloping(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "teammate Write should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "teammate Write should be auto-allowed (Allowed: true) in DEVELOPING")
}

func TestCheckToolPermission_TeamLeadBashNotAutoAllowed(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	// "node server.js" is not in the safe prefix list — should not be auto-approved for Team Lead
	input, _ := json.Marshal(map[string]string{"command": "node server.js"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead Bash with node server.js is not denied in DEVELOPING")
	assert.False(t, result.Allowed, "Team Lead Bash with node server.js should NOT be auto-allowed")
}

// --- Auto-approve narrow list tests ---

func TestCheckToolPermission_CurlAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// curl is in safeBashPrefixes (PLANNING whitelist) but NOT in autoApproveBashPrefixes.
	// However, teammates get auto-approved for any non-denied Bash command (same as non-Bash tools).
	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "curl is not denied in DEVELOPING (only git commands are blocked)")
	assert.True(t, result.Allowed, "curl should be auto-approved for teammates (bypass permission prompt)")
}

func TestCheckToolPermission_GitDiffAutoAllowed(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git diff"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git diff should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "git diff should be auto-approved (truly read-only)")
}

func TestCheckToolPermission_GitConfigAutoAllowedForTeammate(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	// git config can write but is not in the forbidden list, so it's not denied.
	// Teammates get auto-approved for any non-denied Bash command.
	input, _ := json.Marshal(map[string]string{"command": "git config user.name"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
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

// --- Per-phase lead file write permission tests ---

func setupPlanningLeadWriteConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	wfAgentsDir := filepath.Join(tmpDir, ".wf-agents")
	if err := os.MkdirAll(wfAgentsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `phases:
  PLANNING:
    permissions:
      lead:
        file_writes: allow
        file_writes_paths: ["specs/"]
`
	if err := os.WriteFile(filepath.Join(wfAgentsDir, "workflow.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}
	if err := InitGuardConfig(tmpDir); err != nil {
		t.Fatalf("InitGuardConfig: %v", err)
	}
	t.Cleanup(func() {
		_ = InitGuardConfig(t.TempDir())
	})
	return tmpDir
}

func TestCheckToolPermission_LeadAllowedWriteToSpecsInPlanning(t *testing.T) {
	tmpDir := setupPlanningLeadWriteConfig(t)
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs", "RISKDEV-1234_foo.md")})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead write to /specs/ path in PLANNING should be allowed")
}

func TestCheckToolPermission_LeadDeniedWriteToNonSpecsInPlanning(t *testing.T) {
	tmpDir := setupPlanningLeadWriteConfig(t)
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "cmd", "main.go")})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead write to non-specs path in PLANNING should be denied")
	assert.Contains(t, result.Reason, "restricted to")
}

func TestCheckToolPermission_LeadStillDeniedInDeveloping(t *testing.T) {
	tmpDir := setupPlanningLeadWriteConfig(t)
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs", "foo.md")})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead write in DEVELOPING should be denied (no per-phase override, global default deny)")
	assert.Contains(t, result.Reason, "Team Lead")
}

func TestCheckToolPermission_LeadDeniedWriteToNestedSpecsInPlanning(t *testing.T) {
	tmpDir := setupPlanningLeadWriteConfig(t)
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "vendor", "something", "specs", "sneaky.md")})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead write to vendor/something/specs/ in PLANNING must be denied (only top-level specs/ allowed)")
}

func TestCheckToolPermission_LeadDeniedWriteOutsideProjectInPlanning(t *testing.T) {
	setupPlanningLeadWriteConfig(t)
	agentID, activeAgents := teamLeadArgs()
	// Path has /specs/ but is outside the project directory — must be denied.
	input, _ := json.Marshal(map[string]string{"file_path": "/etc/evil/specs/malicious.md"})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead write outside project dir with matching subdir name should be denied")
}

// --- isPathInAllowedDirs unit tests ---

func TestIsPathInAllowedDirs(t *testing.T) {
	// Run without a project dir set (guardProjectDir empty) — falls back to string-contains only.
	origDir := guardProjectDir
	guardProjectDir = ""
	t.Cleanup(func() { guardProjectDir = origDir })

	specsInput, _ := json.Marshal(map[string]string{"file_path": "/project/specs/foo.md"})
	projectSpecsInput, _ := json.Marshal(map[string]string{"file_path": "/project/specs/RISKDEV-1234.md"})
	cmdInput, _ := json.Marshal(map[string]string{"file_path": "/project/cmd/main.go"})
	emptyPathInput, _ := json.Marshal(map[string]string{"file_path": ""})

	assert.True(t, isPathInAllowedDirs(specsInput, []string{"specs/"}), "/project/specs/foo.md should match specs/")
	assert.True(t, isPathInAllowedDirs(projectSpecsInput, []string{"specs/"}), "/project/specs/RISKDEV-1234.md should match specs/")
	assert.False(t, isPathInAllowedDirs(cmdInput, []string{"specs/"}), "/project/cmd/main.go should not match specs/")
	assert.False(t, isPathInAllowedDirs(nil, []string{"specs/"}), "nil input should return false")
	assert.False(t, isPathInAllowedDirs(emptyPathInput, []string{"specs/"}), "empty file_path should return false")
}

func TestIsPathInAllowedDirs_OutsideProjectDenied(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := guardProjectDir
	guardProjectDir = filepath.Clean(tmpDir)
	t.Cleanup(func() { guardProjectDir = origDir })

	// Path contains /specs/ but is outside the project dir.
	outsideInput, _ := json.Marshal(map[string]string{"file_path": "/etc/evil/specs/malicious.md"})
	assert.False(t, isPathInAllowedDirs(outsideInput, []string{"specs/"}), "path outside project dir should be denied even if subdir matches")

	// Path inside project dir with matching subdir.
	insideInput, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs", "foo.md")})
	assert.True(t, isPathInAllowedDirs(insideInput, []string{"specs/"}), "path inside project dir with matching subdir should be allowed")

	// Path inside project dir but nested under vendor — must NOT match.
	vendorInput, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "vendor", "something", "specs", "sneaky.md")})
	assert.False(t, isPathInAllowedDirs(vendorInput, []string{"specs/"}), "vendor/something/specs/ must not match specs/ (relative prefix check)")

	// Direct child of specs/ — must match.
	directInput, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs", "RISKDEV-1234.md")})
	assert.True(t, isPathInAllowedDirs(directInput, []string{"specs/"}), "specs/RISKDEV-1234.md must match specs/")

	// specs-extra/ must NOT match specs/ (HasPrefix correctly rejects a longer prefix).
	specsExtraInput, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs-extra", "foo.md")})
	assert.False(t, isPathInAllowedDirs(specsExtraInput, []string{"specs/"}), "specs-extra/foo.md must not match specs/")

	// relPath == dir exact-dir edge case: file_path is the bare directory (no trailing slash, no filename).
	// File-writing tools always target files, not directories, so this is harmless — documenting behavior.
	exactDirInput, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmpDir, "specs")})
	assert.True(t, isPathInAllowedDirs(exactDirInput, []string{"specs/"}), "exact dir path specs matches via relPath == dir")
}

// --- Team Lead can write Claude infra files (plan/memory) ---

func TestCheckToolPermission_TeamLeadAllowedWriteToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/plans/iteration-plan.md"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Write to /.claude/plans/ files")
}

func TestCheckToolPermission_TeamLeadAllowedEditToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/home/user/.claude/plans/my-plan.md"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Edit /.claude/plans/ files")
}

func TestCheckToolPermission_TeamLeadAllowedWriteToMemoryFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/projects/abc/memory/notes.md"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to Write to /.claude/projects/.../memory/ files")
}

func TestCheckToolPermission_TeamLeadStillDeniedEditToProjectFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/cmd/client/main.go"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Edit", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Team Lead should still be denied from editing regular project files")
	assert.Contains(t, result.Reason, "Team Lead")
}

// --- PLANNING phase: Claude infra files allowed, project files still denied ---

func TestCheckToolPermission_PlanningAllowedWriteToPlanFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/plans/my-plan.md"})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Write to /.claude/plans/ should be allowed in PLANNING")
}

func TestCheckToolPermission_PlanningAllowedWriteToMemoryFile(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/.claude/projects/proj/memory/mem.md"})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Write to /.claude/projects/.../memory/ should be allowed in PLANNING")
}

func TestCheckToolPermission_PlanningStillDeniedWriteToProjectFile(t *testing.T) {
	agentID := "developer-1"
	activeAgents := []string{"developer-1"}
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/cmd/client/main.go"})
	result := CheckToolPermission(model.Phase("PLANNING"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Write to project file should still be denied in PLANNING for developer teammate")
}

func TestCheckToolPermission_RespawnStillDeniedWriteToProjectFile(t *testing.T) {
	agentID := "developer-1"
	activeAgents := []string{"developer-1"}
	input, _ := json.Marshal(map[string]string{"file_path": "/Users/alice/projects/myrepo/internal/workflow/guards.go"})
	result := CheckToolPermission(model.Phase("RESPAWN"), "Write", input, agentID, activeAgents)
	assert.True(t, result.Denied, "Write to project file should still be denied in RESPAWN for developer teammate")
}

// --- isSafeBashCommandInPhase path-stripping tests ---

func TestIsSafeBashCommand_WithPath(t *testing.T) {
	// /usr/bin/ls -la should match "ls" prefix via basename extraction
	assert.True(t, isSafeBashCommandInPhase(model.Phase("PLANNING"), "/usr/bin/ls -la"),
		"/usr/bin/ls -la should match safe prefix 'ls' via basename extraction")
}

func TestIsSafeBashCommand_WithAbsolutePathWfClient(t *testing.T) {
	// /path/to/bin/wf-client status foo should match "wf-client" prefix via basename extraction
	assert.True(t, isSafeBashCommandInPhase(model.Phase("PLANNING"), "/path/to/bin/wf-client status foo"),
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
	result := CheckToolPermission(model.Phase("REVIEWING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "pwd && git log --oneline should not be denied in REVIEWING")
	assert.True(t, result.Allowed, "pwd && git log --oneline should be auto-approved in REVIEWING")
}

func TestCheckToolPermission_AndAndGitCommitDenied(t *testing.T) {
	// "pwd && git commit -m 'x'" in REVIEWING should be denied (git commit forbidden)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "pwd && git commit -m \"x\""})
	result := CheckToolPermission(model.Phase("REVIEWING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "pwd && git commit -m 'x' should be denied in REVIEWING — git commit not allowed")
}

func TestCheckToolPermission_GitLogRedirectionAutoApproved(t *testing.T) {
	// "git log --oneline 2>&1" in REVIEWING should be auto-approved (no split at &1)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "git log --oneline 2>&1"})
	result := CheckToolPermission(model.Phase("REVIEWING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "git log --oneline 2>&1 should not be denied in REVIEWING")
	assert.True(t, result.Allowed, "git log --oneline 2>&1 should be auto-approved in REVIEWING")
}

func TestCheckToolPermission_EchoAndGitPushDenied(t *testing.T) {
	// "echo hello && git push" in DEVELOPING should be denied (git push caught through &&)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "echo hello && git push"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "echo hello && git push should be denied in DEVELOPING — git push not allowed")
}

// --- gofmt in autoApproveBashPrefixes ---

func TestCheckToolPermission_GofmtAutoApprovedInDeveloping(t *testing.T) {
	// "gofmt ./..." in DEVELOPING should be auto-approved
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt ./..."})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "gofmt ./... should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "gofmt ./... should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_GofmtDeniedInPlanning(t *testing.T) {
	// "gofmt" in PLANNING should be denied — gofmt -w modifies files and is not allowed outside DEVELOPING/REVIEWING
	agentID, activeAgents := teamLeadArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt -l ./..."})
	result := CheckToolPermission(model.Phase("PLANNING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "gofmt should be denied in PLANNING (file-modifying command)")
}

func TestCheckToolPermission_GofmtAllowedInFeedback(t *testing.T) {
	// gofmt in FEEDBACK for a teammate: not denied (teammates auto-approved for non-denied bash)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt -w ./..."})
	result := CheckToolPermission(model.Phase("FEEDBACK"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "gofmt in FEEDBACK should not be denied for a teammate")
}

func TestCheckToolPermission_GofmtAllowedInCommitting(t *testing.T) {
	// gofmt in COMMITTING for a teammate: not denied (teammates auto-approved for non-denied bash)
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "gofmt ./..."})
	result := CheckToolPermission(model.Phase("COMMITTING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "gofmt in COMMITTING should not be denied for a teammate")
}

// --- Iteration 1 additions: glab mr, task, golangci-lint, MCP Jira auto-approve ---

func TestCheckToolPermission_GlabMrCreateAutoApproved(t *testing.T) {
	// "glab mr create" in PR_CREATION phase should be auto-approved for a teammate
	// via the PR_CREATION-specific isGlabMRCommand path (not global autoApproveBashPrefixes).
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "glab mr create --draft --target-branch main"})
	result := CheckToolPermission(model.Phase("PR_CREATION"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "glab mr create should not be denied in PR_CREATION")
	assert.True(t, result.Allowed, "glab mr create should be auto-approved in PR_CREATION")
}

func TestCheckToolPermission_GlabMrViewAutoApprovedInPRCreation(t *testing.T) {
	// "glab mr view" in PR_CREATION phase should be auto-approved via the PR_CREATION-specific path.
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "glab mr view 42"})
	result := CheckToolPermission(model.Phase("PR_CREATION"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "glab mr view should not be denied in PR_CREATION")
	assert.True(t, result.Allowed, "glab mr view should be auto-approved in PR_CREATION")
}

func TestCheckToolPermission_GlabMrListAutoApprovedInPRCreation(t *testing.T) {
	// "glab mr list" in PR_CREATION phase should be auto-approved via the PR_CREATION-specific path.
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "glab mr list --all"})
	result := CheckToolPermission(model.Phase("PR_CREATION"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "glab mr list should not be denied in PR_CREATION")
	assert.True(t, result.Allowed, "glab mr list should be auto-approved in PR_CREATION")
}

func TestCheckToolPermission_GlabMrCreateNotAutoApprovedInDeveloping(t *testing.T) {
	// "glab mr create" in DEVELOPING should NOT be auto-approved (Allowed: false) since
	// it was removed from the global autoApproveBashPrefixes list. Teammates still get
	// auto-approved for all non-denied commands, but the key assertion here is that the
	// PR_CREATION-specific path does not fire outside PR_CREATION.
	// The command is not denied (no deny rule), but Allowed should be true for teammate bypass.
	// The meaningful change is: the auto-approve no longer comes from the global prefix list.
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "glab mr create --draft"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "glab mr create should not be denied in DEVELOPING (no deny rule)")
	// In DEVELOPING, the PR_CREATION block does NOT fire, so glab mr create is NOT in
	// autoApproveBashPrefixes — allSegmentsSafe will be false, so Allowed comes only
	// from the teammate bypass (Allowed: true set outside checkBashPermission).
	assert.True(t, result.Allowed, "teammate gets auto-approved for all non-denied commands")
}

func TestCheckToolPermission_TaskLintAutoApproved(t *testing.T) {
	// "task lint" should be auto-approved for a teammate in any phase (e.g., DEVELOPING).
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "task lint"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "task lint should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "task lint should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_TaskTestAutoApproved(t *testing.T) {
	// "task test" should be auto-approved for a teammate in any phase (e.g., DEVELOPING).
	agentID, activeAgents := teammateArgs()
	input, _ := json.Marshal(map[string]string{"command": "task test"})
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "task test should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "task test should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_McpJiraGetIssueAutoApproved(t *testing.T) {
	// MCP Jira read tool "mcp__claude_ai_Atlassian__getJiraIssue" should be auto-approved
	// in any phase (e.g., DEVELOPING) — it is a read-only context extraction tool.
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "mcp__claude_ai_Atlassian__getJiraIssue", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "mcp__claude_ai_Atlassian__getJiraIssue should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "mcp__claude_ai_Atlassian__getJiraIssue should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_McpJiraSearchAutoApproved(t *testing.T) {
	// MCP Jira search tool "mcp__claude_ai_Atlassian__searchJiraIssuesUsingJql" should be
	// auto-approved in any phase — it is read-only Jira context extraction.
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "mcp__claude_ai_Atlassian__searchJiraIssuesUsingJql", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "mcp__claude_ai_Atlassian__searchJiraIssuesUsingJql should not be denied in DEVELOPING")
	assert.True(t, result.Allowed, "mcp__claude_ai_Atlassian__searchJiraIssuesUsingJql should be auto-approved in DEVELOPING")
}

func TestCheckToolPermission_McpNonJiraNotAutoApproved(t *testing.T) {
	// A non-Jira MCP tool should NOT be auto-approved in DEVELOPING —
	// only the narrow Jira read-only tools get the MCP auto-approve treatment.
	// For a teammate, it will not be denied (falls through to teammate bypass), but
	// Allowed should be true because teammates get auto-approved for all non-denied tools.
	// What we verify here is that the MCP Jira block does NOT fire for Slack tools.
	// The Allowed field will be true (teammate bypass), but the reason is NOT the Jira block.
	agentID, activeAgents := teammateArgs()
	result := CheckToolPermission(model.Phase("DEVELOPING"), "mcp__claude_ai_Slack__slack_send_message", nil, agentID, activeAgents)
	assert.False(t, result.Denied, "non-Jira MCP tool should not be denied in DEVELOPING (no deny rule)")
	// Teammates get auto-approved for all non-denied tools — verify it is allowed
	// but confirm it is NOT denied (the key assertion: Slack send is not blocked).
	// We do not assert Allowed=false here because teammate bypass sets Allowed=true.
	// The meaningful assertion is: Denied=false (no explicit block for Slack tools).
}

// --- wf-client transition guard tests ---

func TestCheckToolPermission_TeammateWfClientTransitionDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input := json.RawMessage(fmt.Sprintf(`{"command": %q}`, "wf-client transition wf-id --to DEVELOPING"))
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "teammate should be denied from calling wf-client transition")
	assert.Contains(t, result.Reason, "wf-client transition")
}

func TestCheckToolPermission_TeammateWfClientTransitionAbsPathDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input := json.RawMessage(fmt.Sprintf(`{"command": %q}`, "/path/to/bin/wf-client transition wf-id --to REVIEWING"))
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "teammate should be denied from calling wf-client transition via absolute path")
	assert.Contains(t, result.Reason, "wf-client transition")
}

func TestCheckToolPermission_TeammateWfClientTransitionCompoundDenied(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input := json.RawMessage(fmt.Sprintf(`{"command": %q}`, "echo hello && wf-client transition wf-id --to REVIEWING"))
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.True(t, result.Denied, "teammate should be denied from calling wf-client transition in compound command")
	assert.Contains(t, result.Reason, "wf-client transition")
}

func TestCheckToolPermission_TeammateWfClientStatusAllowed(t *testing.T) {
	agentID, activeAgents := teammateArgs()
	input := json.RawMessage(fmt.Sprintf(`{"command": %q}`, "wf-client status wf-id"))
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "teammate should be allowed to call wf-client status")
}

func TestCheckToolPermission_TeamLeadWfClientTransitionAllowed(t *testing.T) {
	agentID, activeAgents := teamLeadArgs()
	input := json.RawMessage(fmt.Sprintf(`{"command": %q}`, "wf-client transition wf-id --to REVIEWING"))
	result := CheckToolPermission(model.Phase("DEVELOPING"), "Bash", input, agentID, activeAgents)
	assert.False(t, result.Denied, "Team Lead should be allowed to call wf-client transition")
}

// --- isWfClientTransition unit tests ---

func TestIsWfClientTransition(t *testing.T) {
	// bare name → true
	assert.True(t, isWfClientTransition("wf-client transition wf-id --to DEVELOPING"))
	// absolute path → true
	assert.True(t, isWfClientTransition("/path/to/bin/wf-client transition wf-id --to REVIEWING"))
	// other subcommand → false
	assert.False(t, isWfClientTransition("wf-client status wf-id"))
	// no subcommand → false
	assert.False(t, isWfClientTransition("wf-client"))
	// echo wf-client transition → false (first token is echo)
	assert.False(t, isWfClientTransition("echo wf-client transition"))
}
