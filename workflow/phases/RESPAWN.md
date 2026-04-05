PHASE: RESPAWN — Shut down old teammates and prepare context for next iteration.

## CHECKLIST

1. **Shutdown old teammates** (if any active from prior iteration):
      1. Send shutdown_request to developer-<N-1> and reviewer-<N-1>
      2. Wait for their shutdown_response confirmations
      3. {{WF_CLIENT}} shut-down <id> --agent developer-<N-1>
      4. {{WF_CLIENT}} shut-down <id> --agent reviewer-<N-1>
      (replace N-1 with the previous iteration number, e.g., developer-1 for iteration 1)
2. **Verify no active teammates remain:** {{WF_CLIENT}} status <id>
3. **Determine current iteration task** from plan (single focused task for this iteration)
4. **Prepare iteration context:** task description, iteration number, any prior rejection feedback
5. **Transition:** {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>" --repo {WORKTREE}

NOTE: This deliberately clears accumulated context window noise from prior iterations.
The current iteration task is the ONLY task the Developer will receive — do not forward the full plan.

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only teammate management and reads.
