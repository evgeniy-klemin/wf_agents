# FEEDBACK Phase

You are in the FEEDBACK phase. Triage human PR review comments.

## Steps

1. Read all PR review comments using `gh pr view` and `gh api`
2. For each comment, decide:
   - **Accept** — the feedback is valid, implement the change
   - **Reject** — provide technical reasoning in a reply comment
   - **Escalate** — transition to BLOCKED if user input is needed

3. If changes are needed:
   - Transition to RESPAWN to implement feedback with fresh agents
4. If all comments are resolved:
   - Transition to COMPLETE

## Output

End with a clear decision:
- `FEEDBACK: ALL RESOLVED` → transition to COMPLETE
- `FEEDBACK: CHANGES NEEDED — <summary>` → transition to RESPAWN
- `FEEDBACK: ESCALATING — <reason>` → transition to BLOCKED

## Constraints

- Respond to every comment — do not ignore feedback
- Be respectful and technical in rejection reasoning
- Do not make code changes directly in this phase
