PHASE: COMMITTING — Git commit and push are ALLOWED.

CHECKLIST:
- [ ] git add <specific files>
- [ ] git commit -m "<clear message>"
- [ ] git push
- [ ] Verify: git status (working tree must be clean)
- [ ] Decide: more iterations or all done?
  - More iterations → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Iteration N+1: <task>"
  - All done → {{WF_CLIENT}} transition <id> --to PR_CREATION --reason "All iterations complete"

VERIFY: You must be on the feature branch (not BASE_BRANCH). Run git branch --show-current to confirm.
If RESPAWN DENIED: max iterations reached — ask the user whether to continue. If yes, run {{WF_CLIENT}} reset-iterations <id> and retry RESPAWN. If no, go to PR_CREATION.
