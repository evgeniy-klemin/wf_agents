---
title: Enforce SendMessage before teammate idle
status: done
completed: 2026-03-21
priority: high
created: 2026-03-21
depends_on: [02-teammate-idle-lint-enforcement]
---

## Problem

In session `coding-session-860bb648`, the developer teammate completed all work (edits + tests passed) but never sent a completion `SendMessage` back to the Team Lead. The Team Lead was in idle state waiting for that message. Result: **deadlock** — both sitting idle forever.

### Root cause (from hook logs + Claude Code binary analysis)

1. Team Lead sent task via `SendMessage` to developer → went idle (correct)
2. Developer was `status: "running"` → message delivered via queue path ("Message queued for delivery at its next tool round")
3. Developer worked, finished, went idle — **without sending SendMessage back**
4. `TeammateIdle` hook fired → hook returned `logged` (exit_code=0) → developer went idle
5. Team Lead woke briefly (11 seconds), found no message, went back to idle
6. User had to manually type "Разработчик не работает уже двадцать минут" to unstick

### Why the second attempt worked

When the Team Lead sent `SendMessage` to the already-stopped developer, Claude Code used the **resume path**: `"Agent was stopped; resumed it in the background with your message."` This re-spawned the developer from transcript with full context. The developer then correctly sent `SendMessage` back → Team Lead woke up → workflow continued autonomously through REVIEWING → COMMITTING → PR_CREATION → FEEDBACK.

### Key Claude Code mechanism (from binary analysis)

```javascript
// SendMessage.call() has two paths:
if (task.status === "running")
    return "Message queued for delivery at its next tool round.";
// vs
return "Agent was stopped; resumed it in the background with your message.";
```

The "resumed" path creates the "Idle (teammate waiting)" terminal state which auto-wakes the Team Lead. The "queued" path does not.

## Solution

Add a new idle check type `send_message` that denies teammate idle unless the teammate has sent a `SendMessage` during its session. This uses the existing config-driven idle enforcement infrastructure (todo/02).

## Files to modify

| File | Change |
|------|--------|
| `internal/config/workflow_defaults.yaml` | Add `send_message` check to DEVELOPING idle rules |
| `internal/config/eval.go` | Add `send_message` check type to `EvalChecks()` |
| `internal/config/config_test.go` | Tests for new check type |
| `cmd/hook-handler/main.go` | Track SendMessage in command tracking; pass to idle context |
| `agents/developer.md` | Strengthen SendMessage instruction |

## Implementation

### Step 1: Track SendMessage in hook-handler

In `cmd/hook-handler/main.go`, `trackPreToolUse()` already tracks tool usage per agent via Temporal signals. Add tracking for `SendMessage` tool:

```go
if toolName == "SendMessage" {
    commandsRan["_sent_message"] = true
}
```

### Step 2: Add `send_message` check type to eval.go

In `internal/config/eval.go`, `EvalChecks()` handles `command_ran` type. Add `send_message`:

```go
case "send_message":
    if !ctx.CommandsRan()["_sent_message"] {
        return check.Message
    }
```

### Step 3: Add idle rule to workflow_defaults.yaml

In DEVELOPING phase idle rules, add `send_message` check for developer agents:

```yaml
idle:
  - agent: "developer*"
    checks:
      - { type: command_ran, category: lint, message: "Run linter before going idle" }
      - { type: command_ran, category: test, message: "Run tests before going idle" }
      - { type: send_message, message: "Send your completion summary to the Team Lead via SendMessage before going idle." }
```

### Step 4: Update developer.md

Strengthen the instruction (line 40) from:
> Then message the Team Lead with your completion summary.

To:
> **CRITICAL: You MUST send a completion summary to the Team Lead via SendMessage before going idle.** Include "BUILD OK" / "TESTS OK" with actual commands run. The workflow cannot continue until you report back. If you go idle without sending SendMessage, the system will deny your idle and remind you.

### Step 5: Tests

- `config_test.go`: Test `EvalChecks` with `send_message` type — denied when `_sent_message` is false, allowed when true

## Verification

```bash
go test ./internal/config/ -v -run TestEvalChecks
go vet ./...
```

Manual: start a workflow session, observe that developer gets "Send your completion summary..." denial when trying to idle without SendMessage, then sends message and successfully goes idle.
