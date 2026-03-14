PHASE: BLOCKED — Team is paused, waiting for user intervention.

CHECKLIST:
- [ ] Explain the blocker clearly and specifically to the user
- [ ] State exactly what is needed — a decision, missing information, or an action only they can take
- [ ] Wait — do NOT attempt to work around the blocker or make assumptions
- [ ] When resolved, transition back to pre-blocked state:
      {{WF_CLIENT}} status <id> to see pre_blocked_phase
      {{WF_CLIENT}} transition <id> --to <pre_blocked_phase> --reason "Blocker resolved: <what changed>"

CONSTRAINTS:
- You can ONLY return to the state you were in before BLOCKED
- Do not attempt to proceed with incomplete information
- Do not work around missing access or approvals
