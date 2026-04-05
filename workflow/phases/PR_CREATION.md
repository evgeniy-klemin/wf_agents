PHASE: PR_CREATION — Create a draft MR on GitLab and hand off to reviewers.

Do NOT create the MR yourself. Instruct the Developer teammate (still alive — do NOT spawn a new one).

MANDATORY: All MRs MUST be created as Draft. Always use the `--draft` flag when creating a new MR. Never publish an MR out of draft status — the author or reviewer will do that manually.

DECISION TREE:

--- Step 1 — Check for existing MR on current branch ---

Instruct Developer to run (pass the command as-is — `$(git branch --show-current)` is a literal shell
expansion that the shell will evaluate at runtime, do NOT pre-expand it):
  glab mr list --source-branch $(git branch --show-current) --state opened --output json

Parse the result. Extract both fields from the JSON if an MR is present:
  - `web_url`  → the MR URL (used in SHARED STEPS as <mr-url>)
  - `iid`      → the MR internal ID (used in glab mr update / glab mr view commands)

=== BRANCH A: MR already exists ===

  A.1 — Fetch the current MR description:
        Run: glab mr view <mr-iid> --output json
        Extract the `description` field from the JSON output.

  A.2 — Decide whether the MR description needs updating:
        Compare the current commit log against the fetched MR description.
        Run: git log {BASE_BRANCH}..HEAD --oneline

        If new commits exist that are NOT reflected in the MR description:
          A.2a — Update the MR description:
                 Instruct Developer to run:
                   glab mr update <mr-iid> --description "<updated body>"
                 Use the same MR body template as Branch B (see below),
                 populated with the current commit log and diff stats.

        If no update is needed:
          A.2b — Proceed as-is with the existing MR.

  The `<mr-url>` for SHARED STEPS is the `web_url` extracted in Step 1.

  --> After MR is confirmed (go to SHARED STEPS below)

=== BRANCH B: No MR exists ===

  B.1 — Extract context for MR description:
        a) Gather the commit log:
             git log {BASE_BRANCH}..HEAD --oneline
        b) Gather the diff stats:
             git diff --stat {BASE_BRANCH}
        Use this information to populate the MR description template below.

  B.2 — Create draft MR:
        Instruct Developer to run:
          glab mr create --draft --target-branch {BASE_BRANCH} \
            --title "<short description>" \
            --description "<body>"

        MR body template:
        ---
        ## Problem
        <What problem does this MR solve?>

        ## Solution
        <How does this MR solve it?>

        ## Changes
        <Bullet list of key changes derived from git log / diff stats>

        ## Test Plan
        - [ ] Linter passed (`task lint` or equivalent)
        - [ ] Tests passed (`task test` or equivalent)
        - [ ] <Any additional manual verification steps>

        ## Notes for Reviewers
        <Anything reviewers should pay special attention to>
        ---

  B.3 — Wait for Developer to report the MR URL.

  --> After MR is created (go to SHARED STEPS below)

=== SHARED STEPS (both branches) ===

1. **Run:** {{WF_CLIENT}} set-mr-url <session-id> --url <mr-url>
2. **Present MR URL** to user
3. **Transition:** {{WF_CLIENT}} transition <session-id> --to FEEDBACK --reason "MR ready: <url>" --repo {WORKTREE}

VERIFY: Current branch must NOT be {BASE_BRANCH}. If it is, a feature branch was not created in PLANNING.
If {BASE_BRANCH} is not main/master, --target-branch is REQUIRED.

BLOCKED ACTIONS: Edit, Write, NotebookEdit, git commit, git push. Only MR commands and teammate communication allowed.
