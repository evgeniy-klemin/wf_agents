---
title: Track lint/test commands per iteration in workflow state
status: planned
priority: high
created: 2026-03-15
depends_on: [01-declarative-guards-config]
---

## Problem

Guards and TeammateIdle checks need to know if lint/tests ran in the current iteration. Currently no tracking.

## Solution

1. Add `commandsRan map[string]bool` to sessionState (category → true)
2. New signal `track-command` with category string
3. Hook-handler matches Bash commands against `tracking.lint`/`tracking.test` patterns from config
4. On match → send signal to Temporal
5. Reset map on entering DEVELOPING (new iteration)
6. Expose in WorkflowStatus for queries

## Files to modify

- `internal/workflow/coding_session.go` — state field, signal, reset
- `internal/model/workflow_input.go` — CommandsRan in WorkflowStatus
- `cmd/hook-handler/main.go` — pattern matching in PreToolUse
