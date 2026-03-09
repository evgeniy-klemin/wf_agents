# wf_agents — Event-Sourced Claude Code Workflows

## What this is

Temporal-based observer/event store for Claude Code autonomous coding sessions. Temporal does NOT orchestrate — Claude Code runs autonomously, hooks send signals to Temporal which tracks state, enforces phase transitions, and provides a web dashboard.

Inspired by [NTCoding/autonomous-claude-agent-team](https://github.com/NTCoding/autonomous-claude-agent-team).

## Project structure

```
cmd/
  worker/          Temporal worker process
  client/          CLI: start, status, timeline, transition, journal, complete, list
  hook-handler/    Bridge: Claude Code hooks → Temporal signals + permission enforcement
  web/             Web dashboard (Go server + embedded static HTML)
internal/
  model/           Phase enum, events, workflow I/O types (state.go, events.go, workflow_input.go)
  workflow/        Main workflow + guards (coding_session.go, guards.go)
templates/         CLAUDE.md template injected into target projects
hooks/             hooks.json for Claude Code hook configuration
claude/agents/     Agent role prompts (team-lead, developer, reviewer)
claude/states/     Phase instruction prompts
```

## Build and run

```bash
docker compose up -d          # Temporal server + UI (port 8080)
make build                    # builds bin/{worker,wf-client,hook-handler,wf-web}
make worker                   # start worker
make web                      # start web dashboard (port 8090)
```

Uses Colima as Docker runtime. If Docker socket stops responding: `colima restart && docker compose up -d`.

## Phase state machine

```
PLANNING → RESPAWN → DEVELOPING → REVIEWING → COMMITTING → PR_CREATION → FEEDBACK → COMPLETE
              ↑          ↑            │            │                                    │
              │          └────────────┘ (reject)   │                                    │
              └────────────────────────────────────┘ (more iterations)                  │
              └─────────────────────────────────────────────────────────────────────────┘ (feedback)

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

Guards support OR conditions via `altEvidenceKey`/`altWantValue`.

## Hook permission enforcement (cmd/hook-handler)

**PLANNING phase** — whitelist approach:
- Only read-only tools allowed (Read, Glob, Grep, WebFetch, etc.)
- Edit/Write/NotebookEdit are denied
- Bash: only safe commands from whitelist (ls, cat, grep, git status/log/diff/branch/checkout, gh, go test, etc.)
- All other Bash commands denied

**RESPAWN phase** — Edit/Write/NotebookEdit denied

**Global git restrictions** — git commit/push/checkout denied everywhere except:
- PLANNING: git checkout allowed (branch creation)
- COMMITTING: git commit, git push allowed

**Auto-BLOCKED**: Stop/Notification/TeammateIdle → transitions to BLOCKED. Any active event (tool use, UserPromptSubmit) → auto-unblocks.

## Key patterns

- Transitions use `UpdateWorkflow` (synchronous allow/deny), not signals
- `WaitForStage: client.WorkflowUpdateStageCompleted` required in UpdateWorkflow options
- Task description set via `set-task` signal on first `UserPromptSubmit`, updates Temporal memo via `workflow.UpsertMemo`
- Web dashboard reads task from Temporal memo in list results (works for completed workflows too)
- Stuck detection: 5 min idle threshold, terminate via `POST /api/terminate/{id}`

## Testing

```bash
go test ./internal/workflow/ -v    # workflow + guard tests
go test ./internal/model/ -v       # state machine tests
```

Tests use `testsuite.TestWorkflowEnvironment`. Evidence maps: `testEvidence` (all pass), `testEvidenceDirty` (dirty working tree for DEVELOPING→REVIEWING).

## Important conventions

- All communication with Temporal uses workflow ID format: `coding-session-{session-id}`
- `templates/CLAUDE.md` is the autonomous workflow protocol injected into target projects — describes phase execution for Team Lead agent
- Phase instructions injected via `additionalContext` in PreToolUse hook responses
- Web UI is a single embedded HTML file (`cmd/web/static/index.html`) using Tailwind CDN
