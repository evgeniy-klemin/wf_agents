# PLANNING Phase

You are in the PLANNING phase. Your goal is to create an implementation plan.

## Steps

1. **Branch setup** — before anything else:
   - Run `git branch --show-current` to determine the current branch
   - Record it as `BASE_BRANCH` (whatever it is — `main`, `develop`, a feature branch, etc.)
   - Create a new feature branch **from the current branch**: `git checkout -b <feature-branch>`
   - NEVER switch to `main`/`master` first — always branch from what is current
   - Remember `BASE_BRANCH` — it will be the PR target in PR_CREATION
2. Analyze the task requirements
3. Explore the existing codebase to understand the current structure
4. Identify files that need to be created or modified
5. Break the task into ordered subtasks
6. Estimate complexity of each subtask
7. Create a clear, actionable plan

## Output

Produce a plan with:
- Summary of changes needed
- Ordered list of subtasks
- Files to create/modify per subtask
- Testing strategy
- Potential risks or blockers

## Constraints

- Read-only: do NOT modify any files
- Do NOT write code yet
- Focus on analysis and planning
