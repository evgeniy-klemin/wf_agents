PHASE: PLANNING — Read-only exploration and planning.

CHECKLIST (in order — do NOT skip steps):
- [ ] Run git branch --show-current → if on main, run git pull; if NOT on main, ask user whether to switch
- [ ] Record BASE_BRANCH (main or current branch per user's choice)
- [ ] Create feature branch: git checkout -b <feature-branch-name>
- [ ] VERIFY: git branch --show-current — confirm on feature branch, NOT BASE_BRANCH
- [ ] Read relevant files, explore codebase structure
- [ ] Identify files to create or modify
- [ ] Break task into ordered iteration subtasks
- [ ] Define testing strategy
- [ ] Write the plan using plan mode — this creates a plan file the user can review
- [ ] Wait for explicit user approval before transitioning — do NOT proceed without it
- [ ] Transition: {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Plan: <summary>"

RULES:
- NEVER commit directly to BASE_BRANCH — all work happens on the feature branch
- Remember BASE_BRANCH — you will need it in PR_CREATION
- Break large tasks into logical iteration blocks: each iteration should produce a coherent,
  committable unit of progress (e.g., "add data model", "add API handler", "add tests and docs").
  Committing incrementally keeps context windows manageable and makes review easier.

BLOCKED ACTIONS: Edit, Write, NotebookEdit, unsafe Bash commands. Only read-only tools allowed.
If transition DENIED (exit 1): read error, adjust plan.
DO NOT offer to clear context or auto-accept edits. Transition to RESPAWN — that is the designed context reset.
