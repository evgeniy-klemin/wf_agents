PHASE: REVIEWING — Reviewer teammate validates code quality.

Do NOT review code yourself. Delegate to the Reviewer teammate.

CHECKLIST:
- [ ] Tell Reviewer to begin review — do NOT provide a file list (Reviewer uses git diff)
- [ ] Wait for Reviewer verdict via message — be patient, do NOT check in or nudge
- [ ] If APPROVED → {{WF_CLIENT}} transition <id> --to COMMITTING --reason "Review approved"
- [ ] If REJECTED → send Reviewer feedback to Developer, then:
      {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Review rejected: <issues>"

BLOCKED ACTIONS: git commit, git push, Edit/Write (for Reviewer).
