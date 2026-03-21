PHASE: BLOCKED — Team is paused, waiting for user intervention.

You transitioned here because you need something from the user.
When the user responds, the workflow will automatically return you to your previous phase.

NOTE: You CANNOT idle without transitioning to BLOCKED first.
The system will DENY your idle attempt with an error.

CHECKLIST:
- [ ] Transition to BLOCKED with a clear reason:
      {{WF_CLIENT}} transition <id> --to BLOCKED --reason "<what you need from the user>"
- [ ] Explain the blocker clearly and specifically to the user
- [ ] State exactly what is needed — a decision, missing information, or an action only they can take
- [ ] Wait — do NOT attempt to work around the blocker or make assumptions

WHEN TO USE BLOCKED:
- You need a decision from the user (plan approval, scope question, etc.)
- A guard denied your transition and you can't fix it yourself
- Any situation where ONLY the user can unblock you

WHEN NOT TO USE BLOCKED:
- Waiting for Developer/Reviewer response — they will message you back
- Between tool calls in normal workflow — keep working
- In FEEDBACK — you MUST run the polling loop (sleep 60, check PR), do NOT skip it by going to BLOCKED

CONSTRAINTS:
- BLOCKED remembers your previous phase. You can ONLY return to that exact phase.
- Do not attempt to proceed with incomplete information
- Do not work around missing access or approvals
