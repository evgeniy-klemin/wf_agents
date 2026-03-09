# REVIEWING Phase

You are in the REVIEWING phase. Review the Developer's code changes.

## Steps

1. Run `git diff BASE_BRANCH..HEAD` to see all changes (where `BASE_BRANCH` is the branch recorded during PLANNING — may be `main` or another branch)
2. For each changed file, analyze:
   - Correctness of implementation
   - Test coverage
   - Code style and conventions
   - Security concerns
   - Unnecessary complexity
3. Run type checks and linting
4. Run the test suite

## Output

End with a clear verdict:
- `VERDICT: APPROVED` if code is ready
- `VERDICT: REJECTED — <specific issues>` if changes are needed

## Constraints

- Do NOT modify any files
- Be specific about issues
- Reference line numbers when pointing out problems
