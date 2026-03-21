# wf-agents — Autonomous Claude Code Workflow Engine

![Workflow Dashboard](docs/workflow_dashboard.png)

Event-sourced state machine for autonomous Claude Code coding sessions. Tracks development phases, enforces rules via hooks, and provides a real-time web dashboard.

## Known Issues

### Kitty terminal freezes with teammates

On macOS, kitty terminal freezes when Agent Teams teammates are running in background. The only confirmed fix is `kitty-unstick` — a script that periodically sends a no-op escape sequence to prevent hanging.

**Install:**
```bash
install -m 755 /dev/stdin ~/.local/bin/kitty-unstick << 'SCRIPT'
#!/usr/bin/env bash
# Periodically resizes kitty split to prevent freezes
# when Claude Code teammates are running.
# Usage: kitty-unstick [interval_seconds]

INTERVAL="${1:-30}"

echo "Kitty anti-freeze running (every ${INTERVAL}s). Ctrl+C to stop."

while sleep "$INTERVAL"; do
  sock=$(ls /tmp/kitty-* 2>/dev/null | head -1)
  if [ -n "$sock" ]; then
    kitty @ --to "unix:$sock" resize-window -i 1 -a horizontal 2>/dev/null
    kitty @ --to "unix:$sock" resize-window -i -1 -a horizontal 2>/dev/null
  fi
done
SCRIPT
```

Run in a separate terminal tab before starting Claude Code:
```bash
kitty-unstick        # default 30 sec interval
kitty-unstick 15     # custom interval
```

## Concept

Claude Code runs autonomously. wf-agents is the observer and enforcer:
- **Hooks** intercept every Claude Code action and validate permissions
- **State machine** tracks the current phase (Planning → Developing → Reviewing → ...)
- **Guards** validate transition preconditions (clean tree, CI passed, PR approved)
- **Web dashboard** displays all sessions in real time
- **Agent Teams** — teammates (Developer, Reviewer) are independent Claude Code sessions, not one-shot subagents. Each has its own context window and runs until idle, then hands off control.

Inspired by [NTCoding/autonomous-claude-agent-team](https://github.com/NTCoding/autonomous-claude-agent-team).

## Phases (default workflow example)

Phases are defined in `workflow/defaults.yaml` and are fully configurable. The default workflow ships with these phases:

```
PLANNING → RESPAWN → DEVELOPING → REVIEWING → COMMITTING → PR_CREATION → FEEDBACK → COMPLETE
              ↑          ↑            │            │                           │
              │          └────────────┘            │                           │
              └────────────────────────────────────┘                           │
              └────────────────────────────────────────────────────────────────┘

Any phase → BLOCKED (pause) → returns to original phase
```

| Phase | Actor | What happens |
|-------|-------|-------------|
| **PLANNING** | Team Lead | Analyze task, create plan, set up branch. Read-only — writes are blocked |
| **RESPAWN** | Team Lead | Shut down old teammates, prepare iteration context |
| **DEVELOPING** | Developer teammate | TDD: tests → code → refactor. Teammates spawned here via TeamCreate + Agent |
| **REVIEWING** | Reviewer teammate | git diff, checklist, tests, linting → APPROVED/REJECTED |
| **COMMITTING** | Developer teammate (on Lead's instruction) | git commit + push. Lead decides: more iterations or PR |
| **PR_CREATION** | Developer teammate (on Lead's instruction) | `glab mr create`, wait for CI |
| **FEEDBACK** | Team Lead | Validate PR comments, reply to each explicitly |
| **COMPLETE** | — | Terminal state |
| **BLOCKED** | — | Pause, waiting for user input |

## Quick Start

```bash
# 0. Enable Agent Teams in ~/.claude/settings.json:
# { "env": { "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1" } }

# 1. Infrastructure
docker compose up -d              # Temporal Server + UI

# 2. Build
make install                      # bin/{worker,wf-client,hook-handler,wf-web,feedback-poll}

# 3. Worker
make worker                       # start Temporal worker

# 4. Use as Claude Code plugin in target project
cd /path/to/target-project
claude --plugin-dir /path/to/wf_agents

# 5. Inside Claude Code, start the workflow
/wf-agents:start-iriski-team --task "Implement feature X"

# 6. Web dashboard (optional)
make web                          # http://localhost:8090
```

## Enforcement

All permissions, guards, and idle rules are defined declaratively in `workflow/defaults.yaml`. The Go code is a generic executor — no phases, transitions, or permissions are hardcoded.

### How it works

`workflow/defaults.yaml` defines:

- **Phases** — start/stop, display metadata, instructions:
  ```yaml
  phases:
    start: PLANNING
    stop: [COMPLETE]
    DEVELOPING:
      display: { label: "Developing", icon: "code", color: "#10b981" }
      instructions: developing.md
  ```

- **Permissions** — safe commands, whitelists, role restrictions:
  ```yaml
  defaults:
    permissions:
      safe_commands: [ls, cat, git status, go test, ...]
      lead:
        file_writes: deny
  COMMITTING:
    permissions:
      whitelist: [git add, git commit, git push]
  ```

- **Transitions** — allowed paths with `when` guards:
  ```yaml
  transitions:
    DEVELOPING:
      - to: REVIEWING
        when: not working_tree_clean
        message: "no changes to review"
  ```

- **Idle rules** — per-phase, per-agent enforcement:
  ```yaml
  DEVELOPING:
    idle:
      - agent: "developer*"
        checks:
          - { type: command_ran, category: test, message: "Run tests before going idle" }
  ```

- **Tracking** — command categories with patterns:
  ```yaml
  tracking:
    test:
      patterns: ["go test", "npm test", "pytest"]
      invalidate_on_file_change: true
  ```

### Project overrides

Projects can override defaults by placing a `.wf-agents/workflow.yaml` in their root. Override rules:
- Phases: merge by name (override replaces fields, not the whole object)
- Transitions: override replaces transitions for the specified phase
- Tracking/idle/permissions: append or override per existing merge logic

Phase instructions can also be overridden per-phase by placing a `.wf-agents/<PHASE>.md` file (e.g. `.wf-agents/DEVELOPING.md`) in the project root. This replaces the default phase instructions for that phase.

### Key enforcement behaviors

- **File writes** — Team Lead never edits files. Developer teammates can only edit in DEVELOPING and COMMITTING phases
- **Git operations** — `git commit/push` only allowed in COMMITTING phase whitelist
- **Bash splitting** — commands chained with `&&`, `||`, `|`, `;` are split and each segment checked independently
- **BLOCKED phase** — any phase can transition to BLOCKED (pause); returns to pre-blocked phase on user input
- **Iteration limits** — soft limit (default 5). `wf-client reset-iterations <id>` resets counter; `totalIterations` preserved for dashboard

## Architecture

```
Claude Code  ──hooks──►  hook-handler  ──signals──►  Temporal Workflow
                              │                            │
                         enforce rules              track state + guards
                              │                            │
                         deny/allow                  event sourcing
                              │                            │
                         inject phase               web dashboard
                         instructions               (localhost:8090)
```

The hook-handler is the bridge between Claude Code and the Temporal workflow. Every tool call triggers a hook → hook-handler checks permissions from `workflow/defaults.yaml` → allows or denies → sends signals to Temporal for state tracking.
