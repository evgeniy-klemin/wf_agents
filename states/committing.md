PHASE: COMMITTING — Git commit and push are ALLOWED.

Do NOT commit yourself. Instruct the Developer teammate (still alive from DEVELOPING — do NOT spawn a new one).

CHECKLIST:
- [ ] Instruct Developer to commit and push with a meaningful commit message:
      Run git add <specific files>, git commit -m "<clear message>", git push,
      verify working tree is clean with git status, then report back.
- [ ] Wait for Developer confirmation that commit and push are done
- [ ] Verify: git status (working tree must be clean)
- [ ] Decide: more iterations or all done?
  - More iterations → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Iteration N+1: <task>"
  - All done → {{WF_CLIENT}} transition <id> --to PR_CREATION --reason "All iterations complete"

VERIFY: You must be on the feature branch (not BASE_BRANCH). Run git branch --show-current to confirm.

MAX-ITERATIONS PROTOCOL (when RESPAWN is DENIED):
1. Ask user: "Max iterations reached. Continue?"
2. If yes: {{WF_CLIENT}} reset-iterations <id>, then retry RESPAWN
3. If no: transition to PR_CREATION instead

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only git operations and teammate communication allowed.
