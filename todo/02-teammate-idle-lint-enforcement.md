---
title: Config-driven TeammateIdle enforcement
status: done
priority: high
created: 2026-03-15
completed: 2026-03-16
depends_on: [01-declarative-guards-config]
---

## Status: DONE (PR #18)

Implemented config-driven idle enforcement with per-agent glob matching.

```yaml
teammate_idle:
  - phase: DEVELOPING
    agent: "developer*"
    checks:
      - type: command_ran
        category: test
        message: "Run tests before going idle"
  - phase: DEVELOPING
    agent: "reviewer*"
    checks: []
  - phase: "*"
    checks: []
```

`FindIdleRule(cfg, phase, agentName)` with 4-level priority: exact phase+agent > exact phase+any > wildcard+agent > wildcard+any.
