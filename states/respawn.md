PHASE: RESPAWN — Shut down old teammates and prepare context for next iteration.

CHECKLIST:
- [ ] Send shutdown_request to existing Developer and Reviewer (if any active teammates)
- [ ] Wait for shutdown confirmations from both before proceeding
- [ ] Determine current iteration task from plan (single focused task for this iteration)
- [ ] Prepare iteration context: task description, iteration number, any prior rejection feedback
- [ ] Transition: {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only teammate management and reads.
