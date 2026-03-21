# wf_agents — Event-Sourced Claude Code Workflow Plugin

## Project structure

### Plugin files
```
.claude-plugin/plugin.json   Plugin manifest
hooks/hooks.json              Hook configuration (all events → bin/hook-handler)
agents/                       Agent definitions with YAML frontmatter
commands/                     Slash commands
workflow/                    Workflow defaults + phase instructions (UPPERCASE .md) — read from disk
```

### Go backend
```
cmd/
  worker/          Temporal worker process
  client/          CLI: start, status, timeline, transition, journal, complete, list, reset-iterations
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

## Agent Teams (experimental)

This plugin uses Claude Code's Agent Teams feature for multi-agent coordination.
Requires Claude Code v2.1.32+ and the experimental flag enabled in settings.json:

```json
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  }
}
```

### Kitty terminal freezes with teammates

On macOS, kitty terminal freezes when teammates are running in background. The only confirmed fix is `kitty-unstick` — run it in a separate terminal tab before starting Claude Code. See README for install instructions.

## Testing

```bash
go test ./internal/workflow/ -v    # workflow + guard tests
go test ./internal/model/ -v       # state machine tests
```

## Key patterns

- Hook-handler uses `$CLAUDE_PLUGIN_ROOT` env var to locate `bin/wf-client` and other resources
- Session marker file in `$TMPDIR/wf-agents-sessions/<session-id>` gates hooks — no marker = no-op
- Transitions use `UpdateWorkflow` (synchronous allow/deny), not signals
- `WaitForStage: client.WorkflowUpdateStageCompleted` required in UpdateWorkflow options
- Task description set via `set-task` signal on first `UserPromptSubmit`
- Phase instructions loaded from `workflow/*.md` and injected as `additionalContext` on every PreToolUse
- Bash commands chained with `&&`, `||`, `|`, `;` are split and each segment checked independently in guards

## Important conventions

- All communication with Temporal uses workflow ID format: `coding-session-{session-id}`
- Web UI is a single embedded HTML file (`cmd/web/static/index.html`) using Tailwind CDN
