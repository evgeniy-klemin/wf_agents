[DONE] # 06 — MR Completion Criteria: Draft → Ready

## Context

MR создаётся как **Draft**. Текущие критерии завершения (`FEEDBACK → COMPLETE`):
`review_approved or merged`.

**Замена:** MR переведён из Draft в Ready **ИЛИ** MR получил Approved.
На статус `merged` можно не смотреть.

## Branch info

- **BASE_BRANCH:** `main`
- **Feature branch:** `feature/mr-completion-criteria`

## Previous attempt issues

- Developer reported "BUILD OK" but working tree was clean — files not saved to disk
- LSP showed assignment mismatches: poll functions had inconsistent return counts between signature and call sites
- **Key rule for retry:** After EACH file edit, verify with `git diff --stat` that changes are on disk

---

## Iteration 1: All changes (single iteration)

### Step 1. `workflow/defaults.yaml` (line 272)

```yaml
# BEFORE (line 272):
      when: review_approved or merged
      message: "PR has not been approved or merged"

# AFTER:
      when: review_approved or mr_ready
      message: "PR has not been approved and MR is still a draft"
```

### Step 2. `cmd/feedback-poll/main.go`

**2a.** Add `MRDraft` to Output struct (line 30 area):

```go
// Add after PRState field:
MRDraft       bool      `json:"mr_draft"`
```

**2b.** Change `pollGitHub` signature (line 134) — add `draft bool` as 3rd return:

```go
// BEFORE:
func pollGitHub(runner platform.CmdRunner) (approvalState, prState string, inlineComments, prComments []Comment, err error) {

// AFTER:
func pollGitHub(runner platform.CmdRunner) (approvalState, prState string, draft bool, inlineComments, prComments []Comment, err error) {
```

- Add `IsDraft bool` to `prView` struct (JSON field: `isDraft`)
- Add `isDraft` to the `--json` flag: `"reviewDecision,state,number,isDraft,headRepository,headRepositoryOwner"`
- Set `draft = prView.IsDraft` after parsing

**2c.** Change `pollGitLab` signature (line 229) — same pattern:

```go
// BEFORE:
func pollGitLab(runner platform.CmdRunner) (approvalState, prState string, inlineComments, prComments []Comment, err error) {

// AFTER:
func pollGitLab(runner platform.CmdRunner) (approvalState, prState string, draft bool, inlineComments, prComments []Comment, err error) {
```

- Add `Draft bool` to `mrView` struct (JSON field: `draft`)
- Set `draft = mrView.Draft` after parsing

**2d.** Update `main()` call sites (lines 67–70) — 6 return values now:

```go
// BEFORE:
approvalState, prState, inlineComments, prComments, pollErr = pollGitHub(platform.RunCmd)

// AFTER:
approvalState, prState, draft, inlineComments, prComments, pollErr = pollGitHub(platform.RunCmd)
```

Same for `pollGitLab` call.

**2e.** Update status logic in `main()` (lines 91–97):

```go
// BEFORE:
status := "ok"
switch {
case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
    status = "approved"
case strings.ToUpper(prState) == "MERGED":
    status = "merged"
}

// AFTER:
status := "ok"
switch {
case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
    status = "approved"
case !draft:
    status = "ready"
}
```

**2f.** Set `out.MRDraft = draft` in the Output construction.

### Step 3. `cmd/feedback-poll/main_test.go`

**3a.** Update `computeStatus` helper (line 428) — new signature with `draft bool`:

```go
// BEFORE:
func computeStatus(plt, approvalState, prState string) string {
    switch {
    case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
        return "approved"
    case prState == "MERGED" || prState == "merged":
        return "merged"
    }
    return "ok"
}

// AFTER:
func computeStatus(plt, approvalState string, draft bool) string {
    switch {
    case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
        return "approved"
    case !draft:
        return "ready"
    }
    return "ok"
}
```

**3b.** Update all `pollGitHub`/`pollGitLab` call sites — add `draft` as 3rd return:

```go
// BEFORE (e.g. line 93):
approvalState, prState, inline, prComments, err := pollGitHub(runner)

// AFTER:
approvalState, prState, draft, inline, prComments, err := pollGitHub(runner)
```

There are ~7 call sites in tests. **ALL** must be updated.

**3c.** Update test JSON fixtures:

- **GitHub:** add `"isDraft": true` or `"isDraft": false` to all `prViewJSON` strings
- **GitLab:** add `"draft": true` or `"draft": false` to all `mrViewJSON` strings

**3d.** Rename `TestStatusLogic_Merged` → `TestStatusLogic_Ready`:

```go
func TestStatusLogic_Ready(t *testing.T) {
    status := computeStatus("github", "REVIEW_REQUIRED", false)
    if status != "ready" {
        t.Errorf("want ready, got %q", status)
    }
}
```

**3e.** Add new tests:

```go
func TestStatusLogic_Draft(t *testing.T) {
    status := computeStatus("github", "REVIEW_REQUIRED", true)
    if status != "ok" {
        t.Errorf("want ok for draft MR, got %q", status)
    }
}
```

**3f.** Update `TestStatusLogic_Approved` call to new signature.

### Step 4. `internal/config/workflow_validate.go` (line 11–20)

Add `"mr_ready": true` to `knownWhenVariables` map.

### Step 5. `internal/workflow/guards_test.go` (lines 151–182)

- `"FEEDBACK PR merged → COMPLETE ALLOW"` → `"FEEDBACK MR ready → COMPLETE ALLOW"`
  - evidence: `{"review_approved": "false", "mr_ready": "true"}`
- `"FEEDBACK neither approved nor merged → COMPLETE DENY"` → `"FEEDBACK neither approved nor ready → COMPLETE DENY"`
  - evidence: `{"review_approved": "false", "mr_ready": "false"}`
- `"FEEDBACK no approval evidence → COMPLETE DENY"` — keep as-is (empty evidence)

### Step 6. `internal/config/workflow_test.go`

- `TestParseWhenExpression_ReviewApprovedOrMerged` → rename to `TestParseWhenExpression_ReviewApprovedOrMRReady`, change `"merged"` → `"mr_ready"` in expression and assertions
- `TestParseWhenExpression_OrExpression` → change `"merged"` → `"mr_ready"` in expression and assertions
- `TestValidateWorkflowConfig_KnownWhenVariablesPasses` → replace `"merged"` with `"mr_ready"` in `knownVars` list
- `TestParseWhenExpression_BareIdentifierMerged` → rename to `TestParseWhenExpression_BareIdentifierMRReady`, change accordingly

### Step 7. `workflow/FEEDBACK.md`

- Line 13: `"approved/merged"` → `"approved or ready"`
- Line 17: `If status is "approved" or "merged"` → `If status is "approved" or "ready"`
- Lines 57–60: update Step 5 text and guard description

### Step 8. `workflow/COMPLETE.md`

- Line 11: `"PR is approved or merged"` → `"PR is approved or MR moved from draft to ready"`

---

## Verification

```bash
go test ./cmd/feedback-poll/ -v
go test ./internal/config/ -v
go test ./internal/workflow/ -v
```

After each file edit: `git diff --stat` to confirm changes are on disk.
