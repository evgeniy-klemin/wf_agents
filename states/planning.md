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
- [ ] Get user approval for the plan
- [ ] Transition: {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Plan: <summary>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit, unsafe Bash commands. Only read-only tools allowed.
If transition DENIED (exit 1): read error, adjust plan.
DO NOT offer to clear context or auto-accept edits. Transition to RESPAWN — that is the designed context reset.
