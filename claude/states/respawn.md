PHASE: RESPAWN — Spawn fresh agents with clean context.

CHECKLIST:
- [ ] Kill existing Developer/Reviewer subagents
- [ ] Prepare iteration context (plan + current iteration info)
- [ ] Spawn fresh agents — DO NOT pass stale context from prior iterations
- [ ] Transition: {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only agent management and reads.
