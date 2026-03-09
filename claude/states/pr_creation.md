# PR_CREATION Phase

You are in the PR_CREATION phase. Create a draft pull request and wait for CI.

## Steps

1. Create a draft PR using `gh pr create --draft` **against `BASE_BRANCH`**
   - `BASE_BRANCH` is the branch that was current when PLANNING started (recorded in step 1 of PLANNING)
   - If `BASE_BRANCH` is NOT `main`/`master`, you MUST use `--base BASE_BRANCH`
   - Example: `gh pr create --draft --base BASE_BRANCH --title "..." --body "..."`
   - Title: concise summary of the changes
   - Body: description, test plan, and iteration summary
2. Wait for CI checks to start and pass
3. Verify PR checks are green

## Output

When the PR is created and CI is passing:
- Transition to FEEDBACK

## Constraints

- Do NOT edit source files
- The PR should be a draft until human review is complete
- Include a test plan in the PR description
