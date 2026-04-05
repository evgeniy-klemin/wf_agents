# ~~Phase instructions: inject only on transitions, not every PreToolUse~~ ✅ DONE

## Problem

Phase instructions (full `workflow/*.md` content) are injected as `additionalContext` on **every** PreToolUse hook call. With hundreds of tool calls per session this massively inflates the context window — each injection is 500–3400 bytes, multiplied by every non-denied tool call.

## Solution

- **PreToolUse** — inject only the preamble (role + deny-rule reminder, ~200 bytes)
- **SessionStart** — inject full PLANNING instructions (initial phase)
- **`wf-client transition`** — print full instructions for the new phase on success

## Implementation

### 1. New shared package `internal/phasedocs/phasedocs.go`

Extract phase instruction loading from `cmd/hook-handler/main.go` into a reusable package.

```go
package phasedocs

// Preamble returns the short role reminder for the given phase.
// Team Lead phases → teamLeadPreamble; teammate phases (DEVELOPING, REVIEWING) → enforcementPreamble.
func Preamble(phase model.Phase) string

// FullInstructions returns preamble + full workflow/<PHASE>.md content with placeholder substitution.
// Checks project-level override at <cwd>/.wf-agents/<PHASE>.md first.
func FullInstructions(phase model.Phase, cwd string) string
```

Move logic from `cmd/hook-handler/main.go:485–541` (`phaseInstructions` function) here.

### 2. Modify `cmd/hook-handler/main.go`

**PreToolUse** (lines 226–276):
- Replace `phaseInstructions(currentPhase, input.CWD)` → `phasedocs.Preamble(phase)`
- Remove second `queryStatus` call (lines 234, 261) — was only needed for re-querying phase for full instructions; preamble uses the phase from the first query at line 183

**SessionStart** (lines 389–418):
- Add `phasedocs.FullInstructions(model.PhasePlanning, input.CWD)` to `AdditionalContext`

**Cleanup:**
- Remove the now-unused `phaseInstructions` function

### 3. Modify `cmd/client/main.go`

**`cmdTransition`** (line 299–311):
- After `TRANSITION ALLOWED` print, also print full instructions for `result.To`:
  ```go
  instructions := phasedocs.FullInstructions(result.To, cwd)
  if instructions != "" {
      fmt.Println(instructions)
  }
  ```
- Use `os.Getwd()` for `cwd` (the client runs in the project directory)

## Files

| File | Action |
|------|--------|
| `internal/phasedocs/phasedocs.go` | **new** — extracted from hook-handler |
| `cmd/hook-handler/main.go` | simplify PreToolUse, enrich SessionStart, remove old function |
| `cmd/client/main.go` | print instructions on transition success |

## Verification

```bash
go test ./internal/...    # existing tests pass
go build ./cmd/...        # all binaries build
```

Manual checks:
- SessionStart → PLANNING.md instructions in output
- `wf-client transition --to DEVELOPING` → DEVELOPING.md printed
- Any PreToolUse → only preamble, no full .md
