PHASE: PR_CREATION — Create draft PR and wait for CI.

CHECKLIST:
- [ ] gh pr create --draft --base BASE_BRANCH --title "<title>" --body "<description>"
- [ ] Present PR URL to user
- [ ] Wait for CI checks to pass
- [ ] Transition: {{WF_CLIENT}} transition <id> --to FEEDBACK --reason "PR created: <url>, CI passing"

VERIFY: Current branch must NOT be BASE_BRANCH. If it is, you forgot to create a feature branch in PLANNING.
If BASE_BRANCH is not main/master, --base is REQUIRED.
