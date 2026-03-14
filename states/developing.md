PHASE: DEVELOPING — Developer teammate implements via TDD.

Do NOT write code yourself. Delegate to the Developer teammate.

CHECKLIST:
- [ ] Create the team (first iteration only — skip if team already exists from a prior cycle):
      TeamCreate(team_name: "feature-team-<session-id>", description: "Feature team for <task>")
- [ ] Spawn fresh Developer and Reviewer teammates (both in same message):
      Agent(subagent_type: "wf-agents:developer", team_name: "feature-team-<session-id>", name: "developer-<N>")
      Agent(subagent_type: "wf-agents:reviewer", team_name: "feature-team-<session-id>", name: "reviewer-<N>")
- [ ] Send Developer the current iteration task verbatim — do NOT paraphrase or summarize
      Include: current iteration task, iteration number, any prior rejection feedback
- [ ] Wait for Developer to message you they are done — be patient, do NOT check in or nudge
- [ ] Leave all changes UNCOMMITTED — Developer must not git add or git commit
- [ ] Transition: {{WF_CLIENT}} transition <id> --to REVIEWING --reason "Development done, iteration N"
- [ ] Only after transition succeeds — notify Reviewer their work can begin

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).
