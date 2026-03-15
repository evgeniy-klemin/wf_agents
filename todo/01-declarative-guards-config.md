---
title: Declarative guards config with Helm-style overrides
status: planned
priority: high
created: 2026-03-15
---

## Problem

Guard logic and tool permission checks are hardcoded in Go (`guards.go`). Adding new checks requires code changes, rebuilds, and redeployment. Different target projects need different rules.

## Solution

Move guard definitions to declarative YAML config:
- Plugin ships `config/defaults.yaml` with built-in guards
- Target project overrides/extends via `.wf-agents.yaml` in project root
- Helm-style deep merge: project values append to defaults, `disabled: true` removes a guard

### Config format

```yaml
tracking:
  lint: ["golangci-lint", "go vet", "make lint"]
  test: ["go test", "make test"]

guards:
  - transition: "DEVELOPING → REVIEWING"
    checks:
      - type: evidence
        key: "working_tree_clean"
        value: "false"
        message: "No uncommitted changes"
      - type: command_ran
        category: lint
        message: "Run lint before review"

  - transition: "* → RESPAWN"
    checks:
      - type: max_iterations
        message: "Max iterations reached"

teammate_idle:
  - match: "developer*"
    checks:
      - type: command_ran
        category: lint
        message: "Run lint before idle"
```

### Check types

| Type | Description |
|---|---|
| `evidence` | Check evidence map from CLI transition |
| `command_ran` | Check if command category was run this iteration |
| `no_active_agents` | Check activeAgents is empty |
| `max_iterations` | Check iteration limit |
| `file_exists` | Check file exists in CWD |
| `command_succeeds` | Run command, check exit 0 |

### Files to modify

- `internal/config/` — new package: types, loader, merger
- `config/defaults.yaml` — new: plugin defaults (embedded via go:embed)
- `internal/workflow/guards.go` — config-driven guard evaluation
- `internal/workflow/coding_session.go` — config signal, command tracking
- `cmd/hook-handler/main.go` — load config, pattern matching
