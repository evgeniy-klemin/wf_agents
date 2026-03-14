PHASE: PR_CREATION — Create draft PR and wait for CI.

Do NOT create the PR yourself. Instruct the Developer teammate (still alive — do NOT spawn a new one).

CHECKLIST:
- [ ] Instruct Developer to create a draft PR:
      gh pr create --draft --base BASE_BRANCH --title "<title>" --body "<description>"
      Developer writes the PR body: what was done, why, and a test plan
- [ ] Wait for Developer to report the PR URL
- [ ] Present PR URL to user
- [ ] Wait for CI checks to pass — if CI fails, instruct Developer to fix and push again
- [ ] Transition: {{WF_CLIENT}} transition <id> --to FEEDBACK --reason "PR created: <url>, CI passing"

VERIFY: Current branch must NOT be BASE_BRANCH. If it is, you forgot to create a feature branch in PLANNING.
If BASE_BRANCH is not main/master, --base is REQUIRED.

BLOCKED ACTIONS: Edit, Write, NotebookEdit, git commit, git push. Only PR commands and teammate communication allowed.
