package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
)

// guardConfig is the loaded transition guard configuration.
// Initialized once at startup from the embedded defaults.yaml.
var guardConfig *config.Config

func init() {
	cfg, err := config.DefaultConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load guard config: %v", err))
	}
	guardConfig = cfg
}

// sessionCheckContext adapts sessionState + evidence to the config.CheckContext interface.
type sessionCheckContext struct {
	evidence    map[string]string
	state       *sessionState
	originPhase string
}

func (c *sessionCheckContext) Evidence() map[string]string  { return c.evidence }
func (c *sessionCheckContext) ActiveAgentCount() int        { return len(c.state.activeAgents) }
func (c *sessionCheckContext) Iteration() int               { return c.state.iteration }
func (c *sessionCheckContext) MaxIterations() int           { return c.state.maxIter }
func (c *sessionCheckContext) OriginPhase() string          { return c.originPhase }
func (c *sessionCheckContext) CommandsRan() map[string]bool { return nil }
func (c *sessionCheckContext) TeammateName() string         { return "" }

// validateTransition checks whether the transition from→to is allowed given the current
// session state and evidence. Returns "" to allow, or a non-empty denial reason.
//
// Special handling:
//   - Any non-terminal phase → BLOCKED is always allowed (skip guard).
//   - BLOCKED → preBlockedPhase is allowed (skip guard). Any other target is denied.
//   - All other transitions are looked up in the config-driven transitions table.
func validateTransition(s *sessionState, from, to model.Phase, evidence map[string]string) string {
	// BLOCKED can only return to preBlockedPhase (checked first to prevent BLOCKED → BLOCKED)
	if from == model.PhaseBlocked {
		if s.preBlockedPhase == "" || to != s.preBlockedPhase {
			return fmt.Sprintf("BLOCKED can only return to %s (the pre-blocked phase)", s.preBlockedPhase)
		}
		return ""
	}

	// Any non-terminal phase → BLOCKED is always allowed
	if to == model.PhaseBlocked {
		if from.IsTerminal() {
			return fmt.Sprintf("workflow already in terminal state %s", from)
		}
		return ""
	}

	// Determine origin phase for max_iterations check (BLOCKED uses preBlockedPhase)
	origin := s.phase
	if origin == model.PhaseBlocked {
		origin = s.preBlockedPhase
	}

	fromStr, toStr := string(from), string(to)

	// Validate that this transition exists in the state machine.
	// Prefer flow snapshot; fall back to guardConfig for workflows started without a snapshot.
	if s.flow != nil {
		if !s.flow.IsValidTransition(fromStr, toStr) {
			return fmt.Sprintf("transition %s → %s is not allowed", from, to)
		}
	} else if !guardConfig.IsValidTransition(fromStr, toStr) {
		return fmt.Sprintf("transition %s → %s is not allowed", from, to)
	}

	// Evaluate any guard rules from the config. Transitions absent from the
	// config have no guard checks and are allowed unconditionally.
	rules := config.FindGuards(guardConfig, fromStr, toStr)
	if len(rules) == 0 {
		return ""
	}

	ctx := &sessionCheckContext{
		evidence:    evidence,
		state:       s,
		originPhase: string(origin),
	}

	for _, rule := range rules {
		if reason := config.EvalChecks(rule.Checks, ctx); reason != "" {
			return reason
		}
	}
	return ""
}

// --- Tool permission enforcement ---
//
// Rules:
// - Team Lead (main agent, not a teammate): file-writing tools (Edit/Write/NotebookEdit) are
//   always forbidden — Team Lead must delegate file changes to Developer teammate.
// - PLANNING and RESPAWN: all file writes (Edit/Write/NotebookEdit) are forbidden for everyone.
// - Global: git commit, git push, git checkout, git add are forbidden in ALL phases
//   except per-phase whitelists from config.
// - No other tool restrictions (BLOCKED, COMPLETE, etc. have no enforcement)

// ToolPermissionResult indicates whether a tool use is allowed.
type ToolPermissionResult struct {
	Denied  bool
	Allowed bool // explicitly auto-approve (bypass permission prompt)
	Reason  string
}

// isFileWritingTool returns true if the tool modifies files, using config.
func isFileWritingTool(toolName string) bool {
	for _, t := range guardConfig.FileWritingTools() {
		if t == toolName {
			return true
		}
	}
	return false
}

// isReadOnlyTool returns true if the tool is read-only, using config.
func isReadOnlyTool(toolName string) bool {
	for _, t := range guardConfig.ReadOnlyTools() {
		if t == toolName {
			return true
		}
	}
	return false
}

// isClaudeInfraFile returns true if toolInput contains a file_path that points to
// a Claude Code infrastructure file (plan or memory files). These are exempt from
// the Team Lead write block and the PLANNING/RESPAWN write block so that Claude Code's
// plan mode and memory system continue to function.
func isClaudeInfraFile(toolInput json.RawMessage) bool {
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return false
	}
	return strings.Contains(input.FilePath, "/.claude/plans/") ||
		(strings.Contains(input.FilePath, "/.claude/projects/") && strings.Contains(input.FilePath, "/memory/"))
}

// CheckToolPermission checks whether a tool is allowed given the phase, tool name,
// agent ID, and the current set of active teammates.
// This centralizes ALL permission logic alongside transition guards.
func CheckToolPermission(
	phase model.Phase,
	toolName string,
	toolInput json.RawMessage,
	agentID string,
	activeAgents []string,
) ToolPermissionResult {
	isTeamLead := !IsTeammate(agentID, activeAgents)

	// Team Lead cannot edit PROJECT files directly — but CAN write plan/memory files
	// (Claude Code infra: plan mode and memory system). Must delegate project file changes
	// to Developer teammate.
	if isTeamLead && guardConfig.LeadFileWritesDenied() && isFileWritingTool(toolName) && !isClaudeInfraFile(toolInput) {
		return ToolPermissionResult{
			Denied: true,
			Reason: "Team Lead cannot edit files directly — delegate to Developer teammate",
		}
	}

	// Read-only tools are always allowed — explicitly auto-approve to bypass permission prompts
	if isReadOnlyTool(toolName) {
		return ToolPermissionResult{Denied: false, Allowed: true}
	}

	// MCP Jira read tools: auto-approve in any phase (read-only context extraction)
	if strings.HasPrefix(toolName, "mcp__") {
		if strings.Contains(toolName, "Atlassian__getJiraIssue") ||
			strings.Contains(toolName, "Atlassian__searchJiraIssues") {
			return ToolPermissionResult{Denied: false, Allowed: true}
		}
	}

	// PLANNING and RESPAWN: project file writes forbidden, but plan/memory files allowed
	// so that Claude Code's plan mode and memory system continue to function.
	if (phase == model.PhasePlanning || phase == model.PhaseRespawn) && isFileWritingTool(toolName) && !isClaudeInfraFile(toolInput) {
		return ToolPermissionResult{
			Denied: true,
			Reason: fmt.Sprintf("File writes are forbidden in %s phase. %s", phase, PhaseHint(phase)),
		}
	}

	// Teammate permissions: config-driven per-phase/per-agent tool restrictions
	if !isTeamLead && !isClaudeInfraFile(toolInput) {
		bashCmd := ""
		if toolName == "Bash" {
			var input struct {
				Command string `json:"command"`
			}
			json.Unmarshal(toolInput, &input)
			bashCmd = strings.TrimSpace(input.Command)
		}
		if rule := config.FindTeammatePermission(guardConfig, string(phase), agentID, toolName, bashCmd); rule != nil {
			allowed := false
			for _, p := range rule.Phases {
				if p == string(phase) {
					allowed = true
					break
				}
			}
			if !allowed {
				msg := rule.Message
				if msg == "" {
					msg = fmt.Sprintf("tool %s not allowed in this phase", toolName)
				}
				return ToolPermissionResult{
					Denied: true,
					Reason: fmt.Sprintf("%s (current phase: %s)", msg, phase),
				}
			}
		}
	}

	// Bash: enforce global git command restrictions with per-phase exemptions
	if toolName == "Bash" {
		result := checkBashPermission(phase, toolInput)
		if result.Denied {
			return result
		}
		if !isTeamLead {
			return ToolPermissionResult{Denied: false, Allowed: true}
		}
		return result
	}

	// If we get here, the tool is allowed. Auto-approve for teammates to bypass permission prompts.
	if !isTeamLead {
		return ToolPermissionResult{Denied: false, Allowed: true}
	}
	return ToolPermissionResult{Denied: false}
}

// IsTeammate returns true if agentID is non-empty.
// Agent Teams teammates may not have fired SubagentStart before PreToolUse, so
// checking against activeAgents is unreliable. Any non-empty agentID is treated
// as a teammate; an empty agentID means the main agent (Team Lead).
func IsTeammate(agentID string, activeAgents []string) bool {
	return agentID != ""
}

// isAutoApproveBashCommand returns true if the command matches a safe command from config.
// Tries both exact prefix match and basename matching for absolute paths.
func isAutoApproveBashCommand(cmd string) bool {
	safeCommands := guardConfig.SafeCommands()
	for _, prefix := range safeCommands {
		if matchesBashPrefix(cmd, prefix) {
			return true
		}
	}
	// Also try basename matching for absolute paths
	firstWord, rest, _ := strings.Cut(cmd, " ")
	if strings.Contains(firstWord, "/") {
		base := filepath.Base(firstWord)
		cmdWithBase := base
		if rest != "" {
			cmdWithBase = base + " " + rest
		}
		for _, prefix := range safeCommands {
			if matchesBashPrefix(cmdWithBase, prefix) {
				return true
			}
		}
	}
	return false
}

// autoApproveGitSubcommands are git subcommands safe to auto-approve (truly read-only).
// These are kept in Go because they represent a fixed set of read-only git operations
// that bypass the permission prompt without needing to be in the PLANNING whitelist.
var autoApproveGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": true, "remote": true, "tag": true,
	"rev-parse": true, "ls-files": true, "ls-tree": true,
	"blame": true, "shortlog": true,
}

func isAutoApproveGitCommand(seg string) bool {
	if !strings.HasPrefix(seg, "git ") {
		return false
	}
	parts := strings.Fields(seg)
	if len(parts) < 2 {
		return false
	}
	// Skip flags to find subcommand
	idx := 1
	for idx < len(parts) && strings.HasPrefix(parts[idx], "-") {
		idx++
		if idx < len(parts) && (parts[idx-1] == "-C" || parts[idx-1] == "-c") {
			idx++
		}
	}
	if idx >= len(parts) {
		return false
	}
	return autoApproveGitSubcommands[parts[idx]]
}

// isPhaseWhitelistedCommand returns true if the command matches the phase whitelist from config.
func isPhaseWhitelistedCommand(phase model.Phase, cmd string) bool {
	whitelist := guardConfig.PhaseWhitelist(string(phase))
	for _, prefix := range whitelist {
		if matchesBashPrefix(cmd, prefix) {
			return true
		}
	}
	// Also try basename matching for absolute paths
	firstWord, rest, _ := strings.Cut(cmd, " ")
	if strings.Contains(firstWord, "/") {
		base := filepath.Base(firstWord)
		cmdWithBase := base
		if rest != "" {
			cmdWithBase = base + " " + rest
		}
		for _, prefix := range whitelist {
			if matchesBashPrefix(cmdWithBase, prefix) {
				return true
			}
		}
	}
	return false
}

// forbiddenGitCommands are git subcommands forbidden globally by default.
// These are kept in Go as a fixed set that the config-driven whitelists override per-phase.
var forbiddenGitCommands = []string{"git commit", "git push", "git checkout", "git add"}

// checkBashPermission enforces bash command restrictions per phase.
func checkBashPermission(phase model.Phase, toolInput json.RawMessage) ToolPermissionResult {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ToolPermissionResult{Denied: true, Reason: "cannot parse Bash command input"}
	}
	cmd := strings.TrimSpace(input.Command)
	if cmd == "" {
		return ToolPermissionResult{Denied: true, Reason: "cannot parse Bash command input"}
	}

	// PLANNING: whitelist approach — only safe commands allowed
	if phase == model.PhasePlanning {
		return checkPlanningBash(cmd)
	}

	// PR_CREATION: auto-approve commands matching safe_commands + PR_CREATION whitelist
	if phase == model.PhasePRCreation {
		allApproved := true
		for _, segment := range splitBashCommands(cmd) {
			seg := strings.TrimSpace(segment)
			if seg == "" {
				continue
			}
			if !isAutoApproveBashCommand(seg) && !isAutoApproveGitCommand(seg) && !isPhaseWhitelistedCommand(phase, seg) {
				allApproved = false
				break
			}
		}
		if allApproved {
			return ToolPermissionResult{Denied: false, Allowed: true}
		}
	}

	// Other phases: blacklist approach — block specific git commands unless in phase whitelist.
	// Split on pipes/chains so "git add . && git commit" is caught.
	allSegmentsSafe := true
	for _, segment := range splitBashCommands(cmd) {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}
		for _, forbidden := range forbiddenGitCommands {
			if matchesBashPrefix(seg, forbidden) {
				if !isPhaseWhitelistedCommand(phase, forbidden) {
					return ToolPermissionResult{
						Denied: true,
						Reason: fmt.Sprintf("%q is not allowed in %s phase. %s", forbidden, phase, PhaseHint(phase)),
					}
				}
			}
		}
		// File-modifying commands restricted to DEVELOPING/REVIEWING
		if phase != model.PhaseDeveloping && phase != model.PhaseReviewing {
			if matchesBashPrefix(seg, "gofmt") {
				return ToolPermissionResult{
					Denied: true,
					Reason: fmt.Sprintf("gofmt is only allowed in DEVELOPING and REVIEWING phases, current phase: %s", phase),
				}
			}
		}
		// Track whether every segment is in the auto-approve list (for auto-allow).
		// Only truly read-only git commands are auto-approved; git config etc. are not.
		isGitReadOnly := isAutoApproveGitCommand(seg)
		if !isAutoApproveBashCommand(seg) && !isGitReadOnly {
			allSegmentsSafe = false
		}
	}

	if allSegmentsSafe {
		return ToolPermissionResult{Denied: false, Allowed: true}
	}
	return ToolPermissionResult{Denied: false}
}

// safeGitStashSubcommands are the stash sub-operations that are read-only.
// Kept in Go because the stash sub-operation check requires multi-word parsing
// that cannot be expressed as simple prefix matching.
var safeGitStashSubcommands = map[string]bool{
	"list": true, "show": true,
}

// checkPlanningBash uses a whitelist: only safe read-only commands in PLANNING.
// Combines safe_commands (global defaults) and PLANNING phase whitelist from config.
func checkPlanningBash(cmd string) ToolPermissionResult {
	// Handle pipes/chains: check each sub-command
	for _, segment := range splitBashCommands(cmd) {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}

		if strings.HasPrefix(seg, "git ") || seg == "git" {
			if !isAllowedGitInPlanning(seg) {
				return ToolPermissionResult{
					Denied: true,
					Reason: fmt.Sprintf(
						"git command %q is not allowed in PLANNING phase — only read-only git operations permitted. Transition to RESPAWN first.",
						seg,
					),
				}
			}
			continue
		}

		// gofmt can write files (-w flag) — deny explicitly in PLANNING even though
		// it is in safe_commands for auto-approval in DEVELOPING/REVIEWING phases.
		if matchesBashPrefix(seg, "gofmt") {
			return ToolPermissionResult{
				Denied: true,
				Reason: fmt.Sprintf("gofmt is only allowed in DEVELOPING and REVIEWING phases, current phase: %s", model.PhasePlanning),
			}
		}

		if isSafeBashCommand(seg) {
			continue
		}

		return ToolPermissionResult{
			Denied: true,
			Reason: fmt.Sprintf(
				"Command %q is not in the allowed list for PLANNING phase — no repository modifications allowed. Transition to RESPAWN to begin development.",
				truncateCmd(seg, 60),
			),
		}
	}

	return ToolPermissionResult{Denied: false}
}

// isAllowedGitInPlanning checks if a git command is allowed in PLANNING.
// Git commands are checked against the PLANNING phase whitelist from config,
// with special handling for "git stash" which requires a safe sub-operation.
func isAllowedGitInPlanning(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return false
	}
	// Skip flags before subcommand (e.g., git -C /path status)
	idx := 1
	for idx < len(parts) && strings.HasPrefix(parts[idx], "-") {
		idx++
		// Skip flag value for flags that take arguments
		if idx < len(parts) &&
			(parts[idx-1] == "-C" || parts[idx-1] == "-c" || parts[idx-1] == "--git-dir" || parts[idx-1] == "--work-tree") {
			idx++
		}
	}
	if idx >= len(parts) {
		return false
	}
	subCmd := parts[idx]

	// "stash" is only allowed with a safe sub-operation (e.g., "stash list", "stash show").
	// Plain "git stash" or "git stash drop/pop/apply" are not read-only.
	if subCmd == "stash" {
		if idx+1 >= len(parts) {
			return false // bare "git stash" is write-like (saves changes)
		}
		return safeGitStashSubcommands[parts[idx+1]]
	}

	// Check against safe_commands (global defaults) — covers git status, log, diff, etc.
	if isAutoApproveBashCommand(cmd) {
		return true
	}

	// Check against PLANNING phase whitelist — covers git checkout, pull, fetch, etc.
	return isPhaseWhitelistedCommand(model.PhasePlanning, cmd)
}

// isSafeBashCommand checks if a non-git command matches any safe prefix for PLANNING.
// Checks both safe_commands (global) and PLANNING whitelist from config.
// It first tries matching the command as-is, then strips path components from
// the first word so that "/usr/bin/ls -la" matches prefix "ls" and
// "/path/to/bin/wf-client status" matches prefix "wf-client".
func isSafeBashCommand(cmd string) bool {
	// Check global safe_commands
	if isAutoApproveBashCommand(cmd) {
		return true
	}
	// Check PLANNING phase whitelist
	if isPhaseWhitelistedCommand(model.PhasePlanning, cmd) {
		return true
	}
	return false
}

// SplitBashCommandsExported is the exported version of splitBashCommands for use by hook-handler.
func SplitBashCommandsExported(cmd string) []string {
	return splitBashCommands(cmd)
}

// splitBashCommands splits a command line on pipes and command separators.
func splitBashCommands(cmd string) []string {
	var parts []string
	var current strings.Builder
	inSingle, inDouble := false, false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteByte(ch)
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteByte(ch)
		case !inSingle && !inDouble && (ch == '|' || ch == ';' || ch == '\n'):
			parts = append(parts, current.String())
			current.Reset()
			// Skip && and ||
			if i+1 < len(cmd) && (cmd[i+1] == '|' || cmd[i+1] == '&') {
				i++
			}
		case !inSingle && !inDouble && ch == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			// Only split on "&&" (logical AND). Single "&" (background) and
			// redirections like "2>&1" are NOT split.
			parts = append(parts, current.String())
			current.Reset()
			i++ // skip second &
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func truncateCmd(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// PhaseHint returns a short guidance message for denied actions in a phase.
// BLOCKED hint is hardcoded since BLOCKED is not a config-driven phase.
func PhaseHint(phase model.Phase) string {
	if phase == model.PhaseBlocked {
		return "You are blocked. Waiting for user to unblock."
	}
	return guardConfig.PhaseHint(string(phase))
}

// matchesBashPrefix checks if a bash command starts with the given prefix at a word boundary.
func matchesBashPrefix(cmd, prefix string) bool {
	if !strings.HasPrefix(cmd, prefix) {
		return false
	}
	if len(cmd) == len(prefix) {
		return true
	}
	c := cmd[len(prefix)]
	return c == ' ' || c == '\t' || c == '|' || c == ';' || c == '&' || c == '\n'
}
