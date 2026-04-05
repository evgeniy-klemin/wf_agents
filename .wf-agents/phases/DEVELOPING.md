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
6. **Early feedback checkpoint:** Transition to BLOCKED to let the user review changes before auto-review:
   `{{WF_CLIENT}} transition <id> --to BLOCKED --reason "Developer finished. Review the changes and unblock to continue to review." --repo {WORKTREE}`
   - Announce to the user: "Developer finished. Please review the changes before auto-review."
   - Wait for user response.
   - **Default: auto-unblock.** When the user responds, automatically proceed to step 7 unless the response explicitly requires otherwise. The user should never need to say "unblock" or "continue" — any response that provides enough context to continue is sufficient.
     - **Rework:** if the response contains explicit change requests — send feedback to Developer, wait for completion, then repeat step 6.
     - **Unclear:** if the response is ambiguous and you cannot autonomously decide how to proceed — ask the user clarifying questions, then re-evaluate.
7. **Proceed from BLOCKED:** Transition back to DEVELOPING:
   `{{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Unblocked, resuming development phase" --repo {WORKTREE}`
8. Transition to REVIEWING:
   `{{WF_CLIENT}} transition <id> --to REVIEWING --reason "Development done, iteration N" --repo {WORKTREE}`
9. **Only after transition succeeds** — notify Reviewer their work can begin.

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).
