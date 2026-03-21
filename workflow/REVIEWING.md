PHASE: REVIEWING — Reviewer teammate validates code quality.

Do NOT review code yourself. Delegate to the Reviewer teammate.

CRITICAL: In REVIEWING you MUST delegate entirely. You must NOT:
- Read code files to form your own opinion
- Suggest changes yourself
- Perform any review work directly

CHECKLIST:
- [ ] Reviewer is already alive — do NOT spawn a new one
- [ ] Tell Reviewer to begin review — the message MUST include the scope of changes (which files, what the plan was)
- [ ] Wait for Reviewer verdict via message — be patient, do NOT check in or nudge
- [ ] If APPROVED → {{WF_CLIENT}} transition <id> --to COMMITTING --reason "Review approved"
- [ ] If REJECTED → send Reviewer feedback to Developer, then:
      {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Review rejected: <issues>"
      (Developer continues fixing in current session — do NOT follow the RESPAWN protocol,
       teammates stay alive)
- [ ] If DEVELOPING transition DENIED due to max iterations: ask user "Max iterations reached. Continue?"
      If yes: {{WF_CLIENT}} reset-iterations <id>, then retry the transition.
      If no: transition to BLOCKED or ask user to stop the session.

BLOCKED ACTIONS: git commit, git push, Edit/Write (for Reviewer).
