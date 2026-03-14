PHASE: DEVELOPING — Developer subagent implements via TDD.

Do NOT write code yourself. Spawn a Developer subagent.
Agent instructions: use .claude/agents/developer.md if it exists, otherwise {{PLUGIN_ROOT}}/agents/developer.md.

CHECKLIST:
- [ ] Load developer agent: .claude/agents/developer.md (project) or {{PLUGIN_ROOT}}/agents/developer.md (plugin default)
- [ ] Spawn Developer subagent with: agent instructions, current iteration task ONLY (not full plan), iteration number, prior rejection feedback
- [ ] Developer writes failing tests
- [ ] Developer implements to pass tests
- [ ] Developer runs all tests (simple commands only)
- [ ] Leave all changes UNCOMMITTED — do not git add or git commit
- [ ] Transition: {{WF_CLIENT}} transition <id> --to REVIEWING --reason "Development done, iteration N"

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).
