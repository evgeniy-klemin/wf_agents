---
title: Declarative guards config with Helm-style overrides
status: planned
priority: high
created: 2026-03-15
---

## Problem

Guard logic and tool permission checks are hardcoded in Go (`guards.go`). This creates several issues:

1. **Language/project coupling** — lint/test patterns (`go vet`, `golangci-lint`) are Go-specific, but the plugin should be language-agnostic
2. **No project customization** — different projects need different rules (e.g., Python project needs `pytest` tracked, not `go test`)
3. **Rigid transitions** — adding/modifying guard checks requires code changes, rebuilds, redeployment
4. **TeammateIdle enforcement** — currently blanket exit-code-2 for developers with no way to know if work is actually done (see session 73433ac2 where developer was blocked indefinitely)
5. **No CI/CD alignment** — project's Makefile lint/test commands should be the same commands checked by guards

## Solution: Two-layer YAML config with Helm-style merge

### Architecture

```
Plugin (defaults)              Target Project (overrides)
config/defaults.yaml    +      .wf-agents.yaml
        │                              │
        └──────── deep merge ──────────┘
                      │
              Effective Config
                      │
          ┌───────────┼───────────┐
          │           │           │
       Guards    TeammateIdle  Tracking
    (transitions)  (idle rules)  (command patterns)
```

### Layer 1: Plugin defaults (`config/defaults.yaml`)

Shipped with the plugin, embedded via `//go:embed`. Contains all built-in guards that the plugin currently enforces in Go code.

```yaml
tracking:
  lint:
    - "golangci-lint"
    - "go vet"
    - "make lint"
    - "npm run lint"
    - "cargo clippy"
    - "eslint"
    - "pylint"
    - "flake8"
  test:
    - "go test"
    - "make test"
    - "npm test"
    - "cargo test"
    - "pytest"
    - "python -m pytest"

guards:
  - from: DEVELOPING
    to: REVIEWING
    checks:
      - type: evidence
        key: "working_tree_clean"
        value: "false"
        message: "No uncommitted changes — there must be changes to review"

  - from: COMMITTING
    to: RESPAWN
    checks:
      - type: evidence
        key: "working_tree_clean"
        value: "true"
        message: "Working tree not clean — commit or stash changes first"
      - type: max_iterations
        message: "Max iterations reached"

  - from: COMMITTING
    to: PR_CREATION
    checks:
      - type: evidence
        key: "working_tree_clean"
        value: "true"
        message: "Working tree not clean — commit or stash changes first"

  - from: RESPAWN
    to: DEVELOPING
    checks:
      - type: no_active_agents
        message: "Shut down old teammates before spawning new ones"

  - from: PR_CREATION
    to: FEEDBACK
    checks:
      - type: evidence
        key: "pr_checks_pass"
        value: "true"
        message: "PR checks have not passed"

  - from: FEEDBACK
    to: COMPLETE
    checks:
      - type: evidence
        key: "pr_approved"
        value: "true"
        alternatives:
          - key: "pr_merged"
            value: "true"
        message: "PR not approved or merged"

  - from: "*"
    to: RESPAWN
    checks:
      - type: max_iterations
        message: "Max iterations reached"

teammate_idle:
  - match: "*"
    checks: []  # by default everyone idles freely
```

### Layer 2: Project overrides (`.wf-agents.yaml`)

Located in target project root (found via CWD). Extends plugin defaults.

```yaml
# Example for a Go project with strict quality gates
tracking:
  lint:
    # These APPEND to plugin defaults
    - "staticcheck"
    - "gofmt"
  test:
    - "go clean -testcache && go test"

guards:
  # ADD checks to existing transition (appends to defaults)
  - from: DEVELOPING
    to: REVIEWING
    checks:
      - type: command_ran
        category: lint
        message: "Run lint before review"
      - type: command_ran
        category: test
        message: "Run tests before review"

  # ADD guard for transition not in defaults
  - from: REVIEWING
    to: COMMITTING
    checks:
      - type: command_ran
        category: test
        message: "Reviewer must verify tests pass"

  # DISABLE a default guard
  # - from: PR_CREATION
  #   to: FEEDBACK
  #   disabled: true

teammate_idle:
  # OVERRIDE: developer must lint before idle
  - match: "developer*"
    checks:
      - type: command_ran
        category: lint
        message: "Run lint before going idle"
  # reviewer idles freely (override default * match)
  - match: "reviewer*"
    checks: []
```

### Real-world example: Developer blocked by TeammateIdle

In session `73433ac2`, Developer-1 finished all work (build passes, tests pass, 5 files modified) and wanted to go idle. But our blanket TeammateIdle exit-code-2 enforcement kept blocking him indefinitely. The developer sent a message to the Team Lead:

> "The TeammateIdle hook will keep blocking me indefinitely until the Temporal workflow phase changes from DEVELOPING. I cannot resolve this myself."

With the declarative config, this is solved cleanly:

**Plugin defaults** (`config/defaults.yaml`):
```yaml
teammate_idle:
  - match: "*"
    checks: []    # everyone idles freely by default
```

**Project override** (`.wf-agents.yaml`):
```yaml
teammate_idle:
  - match: "developer*"
    checks:
      - type: command_ran
        category: lint
        message: "Run lint before going idle"
```

**How it works:**
1. Developer starts working in DEVELOPING phase
2. Developer runs `golangci-lint run` → hook-handler matches pattern from `tracking.lint`, sends `track-command` signal → `commandsRan["lint"] = true`
3. Developer finishes, goes idle → TeammateIdle fires → checks `command_ran` for `lint` → `commandsRan["lint"]` is true → **idle allowed**
4. If developer tries to idle WITHOUT running lint → check fails → exit code 2 → "Run lint before going idle" → developer keeps working

**Without `.wf-agents.yaml`:** Plugin defaults apply — everyone idles freely, no enforcement. This is the safe default for projects that don't need strict quality gates.

### Merge strategy (Helm-like)

| Section | Merge rule |
|---|---|
| `tracking` lists | Project values **append** to defaults (union, deduplicated) |
| `guards` list | Project guards **append** — same from+to pair = checks combined |
| `guards` with `disabled: true` | **Removes** all guards for that from+to pair |
| `teammate_idle` | Project rules **override** by `match` pattern |

### Check types

| Type | Fields | Description |
|---|---|---|
| `evidence` | `key`, `value`, `alternatives` | Check evidence map passed via CLI `--evidence` flag |
| `command_ran` | `category` | Check if Bash command matching category patterns was run this iteration |
| `no_active_agents` | — | Check `len(activeAgents) == 0` |
| `max_iterations` | — | Check iteration counter vs limit |
| `file_exists` | `path` | Check file exists in CWD |
| `command_succeeds` | `command` | Run shell command, check exit code 0 |

Extensible — new check types added in Go, immediately usable in YAML without config format changes.

### Wildcard transitions

`*` matches any phase:
- `from: "*"` matches any source phase, `to: "*"` matches any target phase. Specific from+to pairs checked first, then wildcards.
- `from: "*", to: RESPAWN` — applies to ALL transitions targeting RESPAWN
- `from: DEVELOPING, to: "*"` — applies to ALL transitions from DEVELOPING

## Implementation plan

### Phase 1: Config package (`internal/config/`)

```
internal/config/
  config.go        — types, LoadConfig(), MergeConfigs()
  defaults.go      — //go:embed config/defaults.yaml
  check_types.go   — check type registry and evaluation functions
  merge.go         — Helm-style merge logic
  config_test.go   — unit tests
```

Key types:
```go
type Config struct {
    Tracking     TrackingConfig `yaml:"tracking"`
    Guards       []GuardRule    `yaml:"guards"`
    TeammateIdle []IdleRule     `yaml:"teammate_idle"`
}

type GuardRule struct {
    From     string  `yaml:"from"`
    To       string  `yaml:"to"`
    Disabled bool    `yaml:"disabled,omitempty"`
    Checks   []Check `yaml:"checks"`
}

type Check struct {
    Type         string `yaml:"type"`
    Key          string `yaml:"key,omitempty"`
    Value        string `yaml:"value,omitempty"`
    Category     string `yaml:"category,omitempty"`
    Command      string `yaml:"command,omitempty"`
    Path         string `yaml:"path,omitempty"`
    Alternatives []KV   `yaml:"alternatives,omitempty"`
    Message      string `yaml:"message"`
}
```

### Phase 2: Iteration command tracking

Add to workflow state:
- `commandsRan map[string]bool` — tracks categories per iteration
- New signal `track-command` — hook-handler sends when Bash matches pattern
- Reset on entering DEVELOPING

### Phase 3: Config-driven guard evaluation

Replace hardcoded guard functions with config evaluation. Keep BLOCKED/terminal logic hardcoded (special cases). Everything else from config.

### Phase 4: Hook-handler integration

- Load effective config (defaults + project override) on first call, cache
- PreToolUse: match Bash commands → send track-command signal
- TeammateIdle: evaluate idle rules from config
- Send config to Temporal via signal on SessionStart

### Phase 5: Migration

Migrate existing hardcoded guards one-by-one to config. Each migration: add to defaults.yaml, remove from Go code, verify tests pass.

## Files to modify

| File | Changes |
|---|---|
| `internal/config/` | New package: types, loader, merger, check evaluation |
| `config/defaults.yaml` | New: plugin defaults (embedded) |
| `internal/workflow/coding_session.go` | Command tracking state, signals, reset |
| `internal/workflow/guards.go` | Config-driven evaluation alongside existing guards |
| `internal/model/workflow_input.go` | CommandsRan in WorkflowStatus |
| `cmd/hook-handler/main.go` | Load config, pattern matching, send signals |
| `states/developing.md` | Note about running project lint |
| `agents/developer.md` | Note about checking .wf-agents.yaml for lint command |

## Backward compatibility

- Without `.wf-agents.yaml` in target project: defaults only (identical to current behavior)
- Existing `--evidence` flags on transitions: unchanged
- Existing hardcoded guards: migrated to defaults.yaml with identical behavior
