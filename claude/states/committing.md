# COMMITTING Phase

You are in the COMMITTING phase. Commit and push approved changes.

## Steps

1. Run `git add` for the changed files
2. Run `git commit` with a clear, descriptive message
3. Run `git push` to the feature branch
4. Verify working tree is clean with `git status`
5. Tick the completed iteration on the issue checklist (if applicable)

## Decision

After committing, decide:
- **More iterations remaining** → transition to RESPAWN
- **All iterations complete** → transition to PR_CREATION

## Constraints

- Only git operations are allowed (commit, push)
- Do NOT edit source files
- Ensure all linted/formatted files are committed
