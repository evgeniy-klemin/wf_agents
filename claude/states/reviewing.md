PHASE: REVIEWING — Reviewer subagent validates code quality.

IF YOU ARE THE TEAM LEAD: Do NOT review code yourself. Spawn a Reviewer subagent.
  Agent instructions: use .claude/agents/reviewer.md if it exists, otherwise {{PLUGIN_ROOT}}/agents/reviewer.md.
IF YOU ARE THE REVIEWER: Read-only. DO NOT modify files. Report verdict.

CHECKLIST:
- [ ] Load reviewer agent: .claude/agents/reviewer.md (project) or {{PLUGIN_ROOT}}/agents/reviewer.md (plugin default)
- [ ] Spawn Reviewer subagent with: agent instructions, scope of changes, plan context
- [ ] Reviewer runs git diff, tests, linting
- [ ] Reviewer outputs VERDICT: APPROVED or VERDICT: REJECTED — <issues>
- [ ] If APPROVED → {{WF_CLIENT}} transition <id> --to COMMITTING --reason "Review approved"
- [ ] If REJECTED → {{WF_CLIENT}} transition <id> --to DEVELOPING --reason "Review rejected: <issues>"

BLOCKED ACTIONS: git commit, git push, Edit/Write (for Reviewer).
