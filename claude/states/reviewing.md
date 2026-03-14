PHASE: REVIEWING — Reviewer subagent validates code quality.

Do NOT review code yourself. Spawn a Reviewer subagent.

CHECKLIST:
- [ ] Spawn Reviewer subagent (subagent_type: "wf-agents:reviewer") with prompt containing: scope of changes, plan context
- [ ] Reviewer runs git diff, tests, linting
- [ ] Reviewer outputs VERDICT: APPROVED or VERDICT: REJECTED — <issues>
- [ ] If APPROVED → {{WF_CLIENT}} transition <id> --to COMMITTING --reason "Review approved"
- [ ] If REJECTED → {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Review rejected: <issues>"

BLOCKED ACTIONS: git commit, git push, Edit/Write (for Reviewer).
