// Package phasedocs provides phase instruction loading for workflow phases.
// It exposes two functions: Preamble (short role reminder) and FullInstructions
// (preamble + full workflow/<PHASE>.md content with placeholder substitution).
package phasedocs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
)

// wfClientBin returns the absolute path to wf-client binary.
// Prefers $CLAUDE_PLUGIN_ROOT/bin/wf-client, falls back to sibling of hook-handler.
func wfClientBin() string {
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		return filepath.Join(root, "bin", "wf-client")
	}
	exe, err := os.Executable()
	if err != nil {
		return "wf-client"
	}
	return filepath.Join(filepath.Dir(exe), "wf-client")
}

// preambleFor returns the short role reminder for the given phase.
// isTeammate=true → enforcementPreamble; isTeammate=false → teamLeadPreamble.
func preambleFor(phase model.Phase, cwd string, isTeammate bool) (string, error) {
	enforcementPreamble := "If a tool call or command fails (denied or exit code != 0), DO NOT retry the same call. Read the error — it contains your next step. Act on it immediately. NEVER go idle after a denial.\n\n"

	if isTeammate {
		return enforcementPreamble, nil
	}

	root, err := config.PluginRoot()
	if err != nil {
		return "", err
	}
	workflowDir := filepath.Join(root, "workflow")
	agentFile, err := config.ResolveFile("team-lead.md", cwd, workflowDir)
	if err != nil {
		return "", fmt.Errorf("team-lead protocol not found: %w", err)
	}

	teamLeadPreamble := "You are the Team Lead. You NEVER write code or review code. You plan, delegate, and coordinate.\n" +
		"If a tool call is denied (permissionDecision: deny) or a transition prints TRANSITION DENIED in stdout, DO NOT retry the same call. Read the output — it tells you what to do next. Act on it immediately. NEVER stop or go idle after a denial.\n" +
		"CONTEXT RECOVERY: If context was compressed or you lost your role instructions, " +
		"re-read your full protocol: " + agentFile + "\n\n"

	return teamLeadPreamble, nil
}

// Preamble returns the short role reminder for the given phase.
// isTeammate=true → enforcementPreamble; isTeammate=false → teamLeadPreamble.
func Preamble(phase model.Phase, cwd string, isTeammate bool) (string, error) {
	return preambleFor(phase, cwd, isTeammate)
}

// FullInstructions returns preamble + full workflow/phases/<PHASE>.md content with placeholder substitution.
// Search order: project (.wf-agents/phases/<PHASE>.md) → preset (<presetDir>/phases/<PHASE>.md) → plugin default (workflow/phases/<PHASE>.md).
// Returns an error if no phase file is found in any location.
func FullInstructions(phase model.Phase, cwd string, isTeammate bool) (string, error) {
	root, err := config.PluginRoot()
	if err != nil {
		return "", err
	}
	wfc := wfClientBin()
	workflowDir := filepath.Join(root, "workflow")
	agentFile, err := config.ResolveFile("team-lead.md", cwd, workflowDir)
	if err != nil {
		return "", fmt.Errorf("team-lead protocol not found: %w", err)
	}

	preamble, err := preambleFor(phase, cwd, isTeammate)
	if err != nil {
		return "", err
	}

	filename := filepath.Join("phases", string(phase)+".md")

	resolvedPath, err := config.ResolveFile(filename, cwd, workflowDir)
	if err != nil {
		return "", fmt.Errorf("phase instructions not found for %s (searched: project .wf-agents/phases/, preset phases/, plugin default %s/workflow/phases/)", phase, root)
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("phase instructions not found for %s (searched: project .wf-agents/phases/, preset phases/, plugin default %s/workflow/phases/)", phase, root)
	}

	content := strings.NewReplacer(
		"{{WF_CLIENT}}", wfc,
		"{{PLUGIN_ROOT}}", root,
		"{{AGENT_FILE}}", agentFile,
	).Replace(string(raw))

	return preamble + content, nil
}
