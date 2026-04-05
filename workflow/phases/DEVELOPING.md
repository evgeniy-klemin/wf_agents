PHASE: DEVELOPING — Developer teammate implements via TDD.

Do NOT write code yourself. Delegate to the Developer teammate.

## CHECKLIST

1. **Create the team** (first iteration only — skip if team already exists from a prior cycle):
      TeamCreate(team_name: "iriski-team-<session-id>", description: "Feature team for <task>")
2. **Spawn fresh Developer and Reviewer teammates** (both in same message):
      Agent(subagent_type: "wf-agents:developer", team_name: "iriski-team-<session-id>", name: "developer-<N>")
      Agent(subagent_type: "wf-agents:reviewer", team_name: "iriski-team-<session-id>", name: "reviewer-<N>")
3. **Send Developer the current iteration task verbatim** — do NOT paraphrase or summarize
      Include: current iteration task, iteration number, any prior rejection feedback,
      and a brief summary of the overall goal (one sentence) for context
4. **Wait for Developer's response** confirming completion ("BUILD OK" / "TESTS OK") — be patient, do NOT check in or nudge
5. **Leave all changes UNCOMMITTED** — Developer must not git add or git commit
6. **Transition:** {{WF_CLIENT}} transition <id> --to REVIEWING --reason "Development done, iteration N" --repo {WORKTREE}
7. **Only after transition succeeds** — notify Reviewer their work can begin

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).
