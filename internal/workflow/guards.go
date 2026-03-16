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

func (c *sessionCheckContext) Evidence() map[string]string    { return c.evidence }
func (c *sessionCheckContext) ActiveAgentCount() int          { return len(c.state.activeAgents) }
func (c *sessionCheckContext) Iteration() int                 { return c.state.iteration }
func (c *sessionCheckContext) MaxIterations() int             { return c.state.maxIter }
func (c *sessionCheckContext) OriginPhase() string            { return c.originPhase }
func (c *sessionCheckContext) CommandsRan() map[string]bool   { return nil }
func (c *sessionCheckContext) TeammateName() string           { return "" }

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
	if !isValidTransition(from, to) {
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

// validTransitions is the set of allowed phase transitions in the state machine.
// BLOCKED transitions are handled specially in validateTransition and not listed here.
var validTransitions = map[model.Phase][]model.Phase{
	model.PhasePlanning:   {model.PhaseRespawn},
	model.PhaseRespawn:    {model.PhaseDeveloping},
	model.PhaseDeveloping: {model.PhaseReviewing},
	model.PhaseReviewing:  {model.PhaseCommitting, model.PhaseDeveloping},
	model.PhaseCommitting: {model.PhaseRespawn, model.PhasePRCreation},
	model.PhasePRCreation: {model.PhaseFeedback},
	model.PhaseFeedback:   {model.PhaseComplete, model.PhaseRespawn},
}

// isValidTransition returns true if from→to is a defined state machine transition.
func isValidTransition(from, to model.Phase) bool {
	for _, allowed := range validTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// --- Tool permission enforcement ---
//
// Rules:
// - Team Lead (main agent, not a teammate): Edit/Write/NotebookEdit are always forbidden —
//   Team Lead must delegate file changes to Developer teammate.
// - PLANNING and RESPAWN: all file writes (Edit/Write/NotebookEdit) are forbidden for everyone.
// - Global: git commit, git push, git checkout, git add are forbidden in ALL phases
//   except per-phase exemptions:
//     PLANNING: git checkout allowed
//     COMMITTING: git add, git commit, git push allowed
// - No other tool restrictions (BLOCKED, COMPLETE, etc. have no enforcement)

// ToolPermissionResult indicates whether a tool use is allowed.
type ToolPermissionResult struct {
	Denied  bool
	Allowed bool // explicitly auto-approve (bypass permission prompt)
	Reason  string
}

// fileWritingTools are tools that modify files.
var fileWritingTools = map[string]bool{
	"Edit": true, "Write": true, "NotebookEdit": true,
}

// forbiddenGitCommands are git subcommands forbidden globally by default.
var forbiddenGitCommands = []string{"git commit", "git push", "git checkout", "git add"}

// gitExemptions lists which git commands are allowed per phase.
var gitExemptions = map[model.Phase][]string{
	model.PhasePlanning:   {"git checkout"},
	model.PhaseCommitting: {"git add", "git commit", "git push"},
}

// readOnlyTools are tools that only read state and never modify files.
var readOnlyTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true,
	"WebFetch": true, "WebSearch": true,
	"ToolSearch": true, "LSP": true,
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
	if isTeamLead && fileWritingTools[toolName] && !isClaudeInfraFile(toolInput) {
		return ToolPermissionResult{
			Denied: true,
			Reason: "Team Lead cannot edit files directly — delegate to Developer teammate",
		}
	}

	// Read-only tools are always allowed — explicitly auto-approve to bypass permission prompts
	if readOnlyTools[toolName] {
		return ToolPermissionResult{Denied: false, Allowed: true}
	}

	// PLANNING and RESPAWN: project file writes forbidden, but plan/memory files allowed
	// so that Claude Code's plan mode and memory system continue to function.
	if (phase == model.PhasePlanning || phase == model.PhaseRespawn) && fileWritingTools[toolName] && !isClaudeInfraFile(toolInput) {
		return ToolPermissionResult{
			Denied: true,
			Reason: fmt.Sprintf("File writes are forbidden in %s phase. %s", phase, PhaseHint(phase)),
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

// safeGitSubcommands are read-only git subcommands allowed in PLANNING.
// For multi-word subcommands like "stash list", only the exact combination is allowed
// (handled separately in isAllowedGitInPlanning).
var safeGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": true, "remote": true, "tag": true, "describe": true,
	"rev-parse": true, "ls-files": true, "ls-tree": true,
	"blame": true, "shortlog": true,
	"config": true, "help": true, "version": true,
	"checkout": true, // allowed in PLANNING for branch creation
	"pull": true, "fetch": true,
}

// safeGitStashSubcommands are the stash sub-operations that are read-only.
var safeGitStashSubcommands = map[string]bool{
	"list": true, "show": true,
}

// autoApproveBashPrefixes is a narrow list of commands safe to auto-approve
// (bypass permission prompts) in any phase. Much smaller than safeBashPrefixes
// which is used for PLANNING whitelist only.
var autoApproveBashPrefixes = []string{
	"go test", "go vet", "go build", "go list", "go mod", "go clean",
	"npm test", "npm run lint", "cargo test", "cargo check",
	"python -m pytest", "pytest",
	"wf-client",
	// Read-only shell utilities safe to auto-approve in any phase
	"ls", "cat", "head", "tail", "wc", "file",
	"grep", "rg", "awk", "sort", "uniq", "diff",
	"echo", "printf", "true", "false", "test", "[",
	"pwd",
	"jq", "yq", "gofmt",
}

func isAutoApproveBashCommand(cmd string) bool {
	for _, prefix := range autoApproveBashPrefixes {
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
		for _, prefix := range autoApproveBashPrefixes {
			if matchesBashPrefix(cmdWithBase, prefix) {
				return true
			}
		}
	}
	return false
}

// autoApproveGitSubcommands are git subcommands safe to auto-approve (truly read-only).
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

// safeBashPrefixes are read-only bash commands allowed in PLANNING.
var safeBashPrefixes = []string{
	"ls", "cat", "head", "tail", "less", "more", "wc", "file",
	"find", "grep", "rg", "ag", "awk", "sort", "uniq", "diff",
	"which", "where", "type", "command", "echo", "printf",
	"pwd", "cd", "tree", "stat", "du", "df",
	"gh pr view", "gh pr list", "gh pr checks", "gh pr diff",
	"gh issue view", "gh issue list",
	"gh api", "gh repo view",
	"go test", "go vet", "go build", "go list", "go mod",
	"npm test", "npm run lint", "npx", "yarn test",
	"make", "cargo test", "cargo check", "cargo clippy",
	"python -m pytest", "pytest", "python -c",
	"jq", "yq", "curl", "wget",
	"env", "printenv", "set", "export",
	"date", "uname", "whoami", "hostname",
	"true", "false", "test", "[",
	"wf-client",
}

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

	// Other phases: blacklist approach — block specific git commands.
	// Split on pipes/chains so "git add . && git commit" is caught.
	exemptions := gitExemptions[phase]
	allSegmentsSafe := true
	for _, segment := range splitBashCommands(cmd) {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}
		for _, forbidden := range forbiddenGitCommands {
			if matchesBashPrefix(seg, forbidden) {
				exempted := false
				for _, ex := range exemptions {
					if ex == forbidden {
						exempted = true
						break
					}
				}
				if !exempted {
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

// checkPlanningBash uses a whitelist: only safe read-only commands in PLANNING.
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

// isAllowedGitInPlanning checks if a git command is safe (read-only) for PLANNING.
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

	return safeGitSubcommands[subCmd]
}

// isSafeBashCommand checks if a command matches any safe prefix for PLANNING.
// It first tries matching the command as-is, then strips path components from
// the first word so that "/usr/bin/ls -la" matches prefix "ls" and
// "/path/to/bin/wf-client status" matches prefix "wf-client".
func isSafeBashCommand(cmd string) bool {
	// First try matching as-is
	for _, prefix := range safeBashPrefixes {
		if matchesBashPrefix(cmd, prefix) {
			return true
		}
	}
	// If first word contains a path, try matching by basename
	firstWord, rest, _ := strings.Cut(cmd, " ")
	if strings.Contains(firstWord, "/") {
		base := filepath.Base(firstWord)
		cmdWithBase := base
		if rest != "" {
			cmdWithBase = base + " " + rest
		}
		for _, prefix := range safeBashPrefixes {
			if matchesBashPrefix(cmdWithBase, prefix) {
				return true
			}
		}
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
func PhaseHint(phase model.Phase) string {
	switch phase {
	case model.PhasePlanning:
		return "Transition to RESPAWN first."
	case model.PhaseRespawn:
		return "Only agent management allowed. Transition to DEVELOPING when agents are ready."
	case model.PhaseReviewing:
		return "Team Lead must delegate review to Reviewer teammate — do NOT review code directly. If issues found, transition back to DEVELOPING (not RESPAWN)."
	case model.PhaseCommitting:
		return "Only git operations are allowed."
	case model.PhasePRCreation:
		return "Only PR creation commands allowed."
	case model.PhaseFeedback:
		return "Triage PR comments. For accepted comments: implement changes first (RESPAWN → DEVELOPING → ... → push), return to FEEDBACK, THEN reply describing what was done and which commit. For rejected comments: reply immediately with technical reasoning. Do NOT reply 'will do X' before doing X — the reply must describe what WAS done."
	case model.PhaseComplete:
		return "Workflow is complete. No further actions needed."
	case model.PhaseBlocked:
		return "Waiting for human intervention. Transition back to the pre-blocked phase when unblocked."
	default:
		return ""
	}
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
