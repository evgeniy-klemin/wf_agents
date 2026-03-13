# wf_agents — Event-Sourced Claude Code Workflow Plugin

Temporal-based observer/event store for Claude Code autonomous coding sessions. Temporal does NOT orchestrate — Claude Code runs autonomously, hooks send signals to Temporal which tracks state, enforces phase transitions, and provides a web dashboard.

## Plugin Format

This project is a Claude Code plugin. Install via:
```bash
make install                          # builds binaries
claude --plugin-dir /path/to/wf_agents
```

### Plugin structure
```
.claude-plugin/plugin.json   Plugin manifest
hooks/hooks.json              Hook configuration (all events → bin/hook-handler)
agents/                       Subagent definitions with YAML frontmatter
  feature-team-lead.md        Team Lead: plans, delegates, coordinates phases
  developer.md                TDD Developer: writes tests first, then implementation
  reviewer.md                 Code Reviewer: validates correctness, reports verdict
commands/                     Slash commands
  start-feature-team.md       /wf-agents:start-feature-team — launch autonomous workflow
  workflow.md                 /wf-agents:workflow — phase transitions
  status.md                   /wf-agents:status — check workflow status
```

### Go backend
```
cmd/
  worker/          Temporal worker process
  client/          CLI: start, status, timeline, transition, journal, complete, list
  hook-handler/    Bridge: Claude Code hooks → Temporal signals + permission enforcement
  web/             Web dashboard (Go server + embedded static HTML)
internal/
  model/           Phase enum, events, workflow I/O types (state.go, events.go, workflow_input.go)
  workflow/        Main workflow + guards (coding_session.go, guards.go)
```

## Build and run

```bash
docker compose up -d          # Temporal server + UI (port 8080)
make install                  # builds bin/{worker,wf-client,hook-handler,wf-web}
make worker                   # start worker
make web                      # start web dashboard (port 8090)
```

Uses Colima as Docker runtime. If Docker socket stops responding: `colima restart && docker compose up -d`.

## Phase state machine

```
PLANNING → RESPAWN → DEVELOPING → REVIEWING → COMMITTING → PR_CREATION → FEEDBACK → COMPLETE
              ↑                       │            │                          │
              ├───────────────────────┘ (reject)   │                          │
              ├────────────────────────────────────┘ (more iterations)        │
              └──────────────────────────────────────────────────────────────┘ (feedback)

Any non-terminal phase → BLOCKED (pause, remembers pre-blocked phase)
BLOCKED → pre-blocked phase only
```

Only COMPLETE is terminal. BLOCKED is a pause, not terminal.

## Transition guards (internal/workflow/guards.go)

Guards validate evidence collected by the client before allowing transitions:
- COMMITTING → any: `working_tree_clean=true`
- DEVELOPING → REVIEWING: `working_tree_clean=false` (must have changes)
- PR_CREATION → FEEDBACK: `pr_checks_pass=true`
- FEEDBACK → COMPLETE: `pr_approved=true` OR `pr_merged=true`

## Hook permission enforcement (cmd/hook-handler)

**PLANNING phase** — whitelist approach:
- Only read-only tools allowed (Read, Glob, Grep, WebFetch, etc.)
- Edit/Write/NotebookEdit are denied
- Bash: only safe commands from whitelist

**RESPAWN phase** — Edit/Write/NotebookEdit denied

**Global git restrictions** — git commit/push/checkout denied everywhere except:
- PLANNING: git checkout allowed (branch creation)
- COMMITTING: git commit, git push allowed

**Team Lead write guard** — Team Lead (main agent, not a subagent) is denied Edit/Write/NotebookEdit in ALL phases. Determined by querying Temporal `activeAgents` and checking if caller's `agentID` is in the list.

**Auto-BLOCKED**: Stop/Notification/TeammateIdle → transitions to BLOCKED. Any active event → auto-unblocks.

## Key patterns

- Hook-handler uses `$CLAUDE_PLUGIN_ROOT` env var to locate `bin/wf-client` and other resources
- Session marker file in `$TMPDIR/wf-agents-sessions/<session-id>` gates hooks — no marker = no-op
- Transitions use `UpdateWorkflow` (synchronous allow/deny), not signals
- `WaitForStage: client.WorkflowUpdateStageCompleted` required in UpdateWorkflow options
- Task description set via `set-task` signal on first `UserPromptSubmit`
- Web dashboard reads task from Temporal memo
- Phase instructions injected as `additionalContext` on every PreToolUse

## Testing

```bash
go test ./internal/workflow/ -v    # workflow + guard tests
go test ./internal/model/ -v       # state machine tests
```

## Important conventions

- All communication with Temporal uses workflow ID format: `coding-session-{session-id}`
- Web UI is a single embedded HTML file (`cmd/web/static/index.html`) using Tailwind CDN
