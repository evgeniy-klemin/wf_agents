PHASE: BLOCKED — Team is paused, waiting for user intervention.

You transitioned here because you need something from the user.
When the user responds, the workflow will automatically return you to your previous phase.

NOTE: You CANNOT idle without transitioning to BLOCKED first.
The system will DENY your idle attempt with an error.

## CHECKLIST

1. **Transition to BLOCKED** with a clear reason:
      {{WF_CLIENT}} transition <id> --to BLOCKED --reason "<what you need from the user>"
2. **Explain the blocker** clearly and specifically to the user
3. **State exactly what is needed** — a decision, missing information, or an action only they can take
4. **Wait for user response** — do NOT attempt to work around the blocker or make assumptions
5. **Auto-unblock when possible.** When the user responds, automatically transition back to the previous phase and continue the workflow — the user should never need to explicitly say "unblock" or "continue". Only stay in BLOCKED if:
   - The user's response contains explicit change requests that require action before proceeding.
   - The response is ambiguous and you cannot autonomously decide how to proceed — ask clarifying questions, then re-evaluate.

CONSTRAINTS:
- BLOCKED remembers your previous phase. You can ONLY return to that exact phase.
- Do not attempt to proceed with incomplete information
- Do not work around missing access or approvals
