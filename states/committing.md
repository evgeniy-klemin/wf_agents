PHASE: COMMITTING — Git commit and push are ALLOWED.

Do NOT commit yourself. Instruct the Developer teammate (still alive from DEVELOPING — do NOT spawn a new one).

CHECKLIST:
- [ ] Instruct Developer to commit and push with a meaningful commit message
      Developer uses: git add <specific files>, git commit -m "<clear message>", git push
- [ ] Wait for Developer confirmation that commit and push are done
- [ ] Verify: git status (working tree must be clean)
- [ ] Decide: more iterations or all done?
  - More iterations → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Iteration N+1: <task>"
  - All done → {{WF_CLIENT}} transition <id> --to PR_CREATION --reason "All iterations complete"

VERIFY: You must be on the feature branch (not BASE_BRANCH). Run git branch --show-current to confirm.
If RESPAWN DENIED: max iterations reached — ask the user whether to continue. If yes, run {{WF_CLIENT}} reset-iterations <id> and retry RESPAWN. If no, go to PR_CREATION.

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only git operations and teammate communication allowed.
