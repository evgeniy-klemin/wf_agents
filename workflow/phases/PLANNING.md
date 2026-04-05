PHASE: PLANNING — Read-only exploration and planning.

## CHECKLIST

1. **Detect worktree:** check the CLAUDE.md path in your system context
   - If it contains `.claude/worktrees/<name>/CLAUDE.md`:
     `{WORKTREE} = .claude/worktrees/<name>/` (absolute path)
     Run: `git -C {WORKTREE} branch --show-current` → record as feature branch
     `{BASE_BRANCH} = main`
     Skip to step 4
   - If NOT: you are in a normal repo. Run `git branch --show-current`
     - If on main → run `git pull`
     - If NOT on main → ask user whether to switch to main or stay on current branch
2. **Record {BASE_BRANCH}** (main, or current branch if user chose to stay)
3. **Create feature branch** (only if no worktree): `git checkout -b <feature-branch-name>`
   `{WORKTREE} = current directory` (absolute path)
4. **VERIFY:** `git -C {WORKTREE} branch --show-current` — confirm on feature branch

Use `{WORKTREE}` for all subsequent `--repo` flags and when telling teammates where to work.

5. **Read relevant files,** explore codebase structure
6. **Identify files** to create or modify
7. **Break task** into ordered iteration subtasks
8. **Define testing strategy**
9. **Grep test files** for usages of functions/structs being modified — if tests will break
      due to changed signatures, removed branches, or new preconditions, list affected
      test call-sites in the plan
10. **Write the plan** using plan mode — this creates a plan file the user can review
## Session variables (persist across all phases)
Record these values — they are referenced in all subsequent phases:
- `{WORKTREE}` = worktree or repo absolute path (from steps 1-3)
- `{BASE_BRANCH}` = main or chosen branch (from step 2)

11. **Wait for explicit user approval** before transitioning — do NOT proceed without it
12. **VERIFY:** git status — working tree must be clean before transition
13. **Transition:** {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Plan: <summary>" --repo {WORKTREE}

RULES:

- NEVER commit directly to {BASE_BRANCH} — all work happens on the feature branch
- Remember {BASE_BRANCH} — you will need it in PR_CREATION
- Break large tasks into logical iteration blocks: each iteration should produce a coherent,
  committable unit of progress (e.g., "add data model", "add API handler", "add tests and docs").
  Committing incrementally keeps context windows manageable and makes review easier.

BLOCKED ACTIONS: Edit, Write, NotebookEdit, unsafe Bash commands. Only read-only tools allowed.
If transition DENIED (stdout says TRANSITION DENIED): read error, adjust plan.
DO NOT offer to clear context or auto-accept edits. Transition to RESPAWN — that is the designed context reset.
