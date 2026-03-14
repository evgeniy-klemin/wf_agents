PHASE: RESPAWN — Spawn fresh teammates with clean context.

CHECKLIST:
- [ ] Send shutdown_request to existing Developer and Reviewer (if any active teammates)
- [ ] Wait for shutdown confirmations from both before proceeding
- [ ] Determine current iteration task from plan (single focused task for this iteration)
- [ ] Spawn fresh teammates (both in same message):
      Agent(subagent_type: "wf-agents:developer", team_name: "feature-team-<session-id>", name: "developer-<N>")
      Agent(subagent_type: "wf-agents:reviewer", team_name: "feature-team-<session-id>", name: "reviewer-<N>")
- [ ] Transition: {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only teammate management and reads.
