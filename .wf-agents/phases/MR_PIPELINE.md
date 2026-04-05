# MR_PIPELINE

You are the Team Lead. Monitor the CI pipeline for the Merge Request.

## CHECKLIST

1. Get MR iid from the previous phase context.
2. Start the polling loop (see below).
3. When the pipeline succeeds (`checks` stage passes) → transition to FEEDBACK.
4. If pipeline fails → handle according to failure type (see below).

## POLLING LOOP

**CRITICAL:** `sleep 60` MUST run in FOREGROUND. Do NOT use run_in_background.

### Loop Pattern

> Each command MUST be a separate Bash tool call. Do NOT chain with `&&`, `||`, `;`, or pipes.

1. `sleep 60` (foreground Bash, separate call)
2. Run `{{PLUGIN_ROOT}}/bin/pipeline-poll --repo {WORKTREE}` (separate Bash call) — outputs structured JSON
3. Parse JSON `status` field:
   - `"running"` → back to Step 1
   - `"success"` → transition to FEEDBACK using `{{WF_CLIENT}} transition <session-id> FEEDBACK`
   - `"failed"` → go to Step 4 (failure triage)
   - `"canceled"` → show `pipeline_url` to user, transition to BLOCKED
   - `"error"` → show `error` field to user, transition to BLOCKED
4. **Failure triage** — use `failed_jobs[]` from JSON output:
   - **Code failures** — show `failed_jobs[].name`, `failed_jobs[].url`, and `failed_jobs[].fail_root_causes`,
     then transition to DEVELOPING using `{{WF_CLIENT}} transition <session-id> DEVELOPING`
     (developer continues fixing in current session — teammates stay alive, do NOT follow RESPAWN protocol).
   - **Infra/flaky failures** — if the failure is unrelated to the code (infra issue, flaky test), transition to BLOCKED using `{{WF_CLIENT}} transition <session-id> BLOCKED`
5. Show `pipeline_url` from JSON output to user for transparency.

## RULES

- Do NOT write code or edit files in this phase.
- If pipeline is stuck > 15 minutes, notify user.
- On failure, provide the failing job name and log URL.
