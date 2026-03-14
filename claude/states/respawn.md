PHASE: RESPAWN — Spawn fresh agents with clean context.

CHECKLIST:
- [ ] Kill existing Developer/Reviewer subagents
- [ ] Determine current iteration task from plan (single focused task for this iteration)
- [ ] Prepare iteration context: current iteration task + iteration number + any prior rejection feedback
- [ ] Spawn fresh agents — DO NOT pass stale context from prior iterations
- [ ] Transition: {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only agent management and reads.
