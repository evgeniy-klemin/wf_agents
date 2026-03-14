PHASE: DEVELOPING — Developer subagent implements via TDD.

IF YOU ARE THE TEAM LEAD: Do NOT write code yourself. Spawn a Developer subagent.
  Agent instructions: use .claude/agents/developer.md if it exists, otherwise {{PLUGIN_ROOT}}/agents/developer.md.
IF YOU ARE THE DEVELOPER: Implement via TDD — tests first, then code, then refactor.
  Use simple, single-purpose Bash commands (go test ./..., npm test, make test).
  For complex commands — create a helper script in scripts/ and run ./scripts/<name>.sh.
  Do NOT use pipes, subshells, or multi-command chains — they block auto-approve.
  Do NOT run git add, git commit, or git push — leave changes uncommitted on disk.
  The REVIEWING guard requires a dirty working tree (uncommitted changes).

CHECKLIST:
- [ ] Load developer agent: .claude/agents/developer.md (project) or {{PLUGIN_ROOT}}/agents/developer.md (plugin default)
- [ ] Spawn Developer subagent with: agent instructions, plan, iteration number, prior rejection feedback
- [ ] Developer writes failing tests
- [ ] Developer implements to pass tests
- [ ] Developer runs all tests (simple commands only)
- [ ] Leave all changes UNCOMMITTED — do not git add or git commit
- [ ] Transition: {{WF_CLIENT}} transition <id> --to REVIEWING --reason "Development done, iteration N"

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).
