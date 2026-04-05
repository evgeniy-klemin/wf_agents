# Implementation — DONE

> All items implemented. Additional work: all hardcoded phase constants and logic removed, made fully config-driven. README documentation added for pipeline-poll and optional MR_PIPELINE phase.

## 1. [DONE] Create `cmd/pipeline-poll/main.go` (Go binary)

Pure Go binary, no external dependencies. Uses `glab api` via `platform.CmdRunner` (same pattern as `feedback-poll`).

### What it does

1. Get current branch (`git rev-parse --abbrev-ref HEAD`)
2. Get latest pipeline for branch (`glab api "projects/:id/pipelines?ref={branch}&per_page=1"`)
3. Get all jobs in pipeline with pagination (`glab api "projects/:id/pipelines/{id}/jobs?per_page=100"`)
4. Determine overall status
5. For failed non-`allow_failure` jobs: fetch full job log (`glab api "projects/:id/jobs/{id}/trace"`)
6. Strip ANSI escape codes from job trace
7. Detect error root causes: grep `error`/`fail`/`panic`/`FATAL` (case-insensitive) with 5 lines before and 5 lines after each match. Collect all matches into `fail_root_causes` array — each element is one match block with context; merge overlapping blocks
8. Store last 50 lines of cleaned log from failed job for output

### Output JSON

```json
{
  "pipeline_id": 12345,
  "pipeline_url": "https://...",
  "created_at": "2026-03-22T12:00:00Z",
  "duration_seconds": 245,
  "status": "running|success|failed|canceled",
  "stages": [
    {"name": "checks", "status": "success"}
  ],
  "failed_jobs": [
    {
      "id": 123,
      "name": "isolation-tests",
      "stage": "checks",
      "url": "https://...",
      "allow_failure": false,
      "fail_root_causes": [
        "...context block 1...",
        "...context block 2..."
      ],
      "log_tail": "..."
    }
  ],
  "all_jobs_done": true
}
```

### Status logic

- Any job running/pending/created → `"running"`
- Pipeline canceled → `"canceled"`
- All passed (or only `allow_failure` failures) → `"success"`
- Otherwise → `"failed"` (agent in target project decides if failure is flaky based on `fail_root_causes`)

### Error output

```json
{"status": "error", "error": "description"}
```

### Functions

- `runGlab(runner, args...)` — execute `glab api`, return stdout
- `getCurrentBranch(runner)` — `git rev-parse`
- `getLatestPipeline(runner, branch)` — fetch latest pipeline
- `getAllJobs(runner, pipelineID)` — paginate jobs
- `getJobLogTail(runner, jobID, lines)` — fetch trace, last N lines
- `stripANSI(text)` — remove ANSI escape codes (`\x1b[...m` etc.) from job trace
- `extractRootCauses(logTail)` — grep `error`/`fail`/`panic`/`FATAL` (case-insensitive), return `[]string` with 5 lines context around each match; merge overlapping blocks
- `computeStages(jobs)` — group by stage, compute per-stage status
- `buildOutput(runner, pipeline, jobs)` — orchestrate and build JSON
- `main()` — entry point, print JSON to stdout

All functions accept `platform.CmdRunner` as first argument for testability.

## 2. [DONE] Create `cmd/pipeline-poll/main_test.go`

Table-driven tests with mock runner (same pattern as `cmd/feedback-poll/main_test.go`):

- Pipeline running → `"running"`
- Pipeline success → `"success"`
- Only `allow_failure` jobs failed → `"success"`
- Pipeline canceled → `"canceled"`
- Real failure → `"failed"` with `failed_jobs` and `fail_root_causes` populated
- No pipeline found → `{"status": "error", ...}`
- `glab api` error → `{"status": "error", ...}`

## 3. [NOT IN PLUGIN DEFAULTS] Create `workflow/MR_PIPELINE.md`

> Optional per-project file — not included in plugin defaults. Documented in README instead.

Phase instructions for Claude Code agent. Replace manual `glab api` chains with single `pipeline-poll` call:

1. `sleep 180` (3 min, foreground, separate Bash call)
2. Run `pipeline-poll` (separate Bash call)
3. Parse JSON:
   - `"running"` → check elapsed time; if < 60 min → back to Step 1; if >= 60 min → transition to BLOCKED with timeout message
   - `"success"` → transition to FEEDBACK
   - `"canceled"` → show info, transition to BLOCKED
   - `"failed"` → show `failed_jobs` with `fail_root_causes` to user, agent decides next action (retry flaky / transition to BLOCKED)
   - `"error"` → show error, transition to BLOCKED

Polling interval: 3 min. Max wait: 60 min (20 iterations). Use `created_at` and `duration_seconds` from output to determine elapsed time.

> Each command MUST be a separate Bash tool call. Do NOT chain with `&&`, `||`, `;`, or pipes.

## 4. [DONE] Modify `workflow/defaults.yaml`

> `pipeline-poll` added to `safe_commands`. `MR_PIPELINE` phase NOT added to defaults — optional per-project.

- Add `MR_PIPELINE` phase section with permissions and idle config:
  ```yaml
  MR_PIPELINE:
    permissions:
      whitelist:
        - glab api
        - pipeline-poll
    idle:
      - agent: lead
        deny: true
        message: "MR_PIPELINE requires active polling..."
  ```
- Add `pipeline-poll` to global `safe_commands` list (alongside `feedback-poll`)
- Add transitions: PR_CREATION → MR_PIPELINE, MR_PIPELINE → FEEDBACK / BLOCKED

## 5. [DONE] Modify `internal/model/state.go`

> All hardcoded phase constants removed — fully config-driven. `PhaseMRPipeline` not added as a constant.

## 6. [DONE] Modify `Makefile`

Add build target:
```makefile
bin/pipeline-poll: $(shell find cmd/pipeline-poll -name '*.go')
	go build -o bin/pipeline-poll ./cmd/pipeline-poll
```

---

## Files

| # | File | Action | Status |
|---|------|--------|--------|
| 1 | `cmd/pipeline-poll/main.go` | Create — Go binary, pattern from `feedback-poll` | DONE |
| 2 | `cmd/pipeline-poll/main_test.go` | Create — tests with mock runner | DONE |
| 3 | `workflow/MR_PIPELINE.md` | Create — phase instructions | NOT IN DEFAULTS (optional per-project, documented in README) |
| 4 | `workflow/defaults.yaml` | Modify — add pipeline-poll to safe_commands | DONE (MR_PIPELINE phase not added — optional) |
| 5 | `internal/model/state.go` | Modify — fully config-driven, hardcoded constants removed | DONE |
| 6 | `Makefile` | Modify — add `bin/pipeline-poll` build target | DONE |

---

## Verification

1. `go build ./cmd/pipeline-poll` — compiles without errors
2. `go test ./cmd/pipeline-poll/ -v` — all test scenarios pass
3. Run `bin/pipeline-poll` on a branch with active pipeline — verify valid JSON output
4. Run on a branch with no pipeline — verify `{"status": "error", ...}` output
5. `go test ./internal/model/ -v` — model tests pass with new phase
6. `go test ./internal/workflow/ -v` — workflow tests pass with new transitions
