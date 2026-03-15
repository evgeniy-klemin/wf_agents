PHASE: RESPAWN — Shut down old teammates and prepare context for next iteration.

CHECKLIST:
- [ ] Deregister old teammates (if any active teammates from prior iteration):
      {{WF_CLIENT}} shut-down <id> --agent developer-<N-1>
      {{WF_CLIENT}} shut-down <id> --agent reviewer-<N-1>
      (replace N-1 with the previous iteration number, e.g., developer-1 for iteration 1)
- [ ] Verify no active teammates remain: {{WF_CLIENT}} status <id>
- [ ] Determine current iteration task from plan (single focused task for this iteration)
- [ ] Prepare iteration context: task description, iteration number, any prior rejection feedback
- [ ] Transition: {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only teammate management and reads.
