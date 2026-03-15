---
title: Config-driven TeammateIdle enforcement
status: planned
priority: high
created: 2026-03-15
depends_on: [01-declarative-guards-config]
---

## Problem

Current TeammateIdle exit-code-2 for developers blocks idle even when work is done. Developer-1 sent message: "The TeammateIdle hook will keep blocking me indefinitely." No way to know if lint/tests were run.

## Solution

Replace blanket enforcement with config-driven checks from `.wf-agents.yaml`:

```yaml
teammate_idle:
  - match: "developer*"
    checks:
      - type: command_ran
        category: lint
        message: "Run lint before going idle"
  - match: "reviewer*"
    checks: []  # reviewer idles freely
```

If no config or no matching rule → idle allowed (no exit 2).

## Files to modify

- `cmd/hook-handler/main.go` — TeammateIdle handler reads config
- `internal/config/` — idle rule evaluation
