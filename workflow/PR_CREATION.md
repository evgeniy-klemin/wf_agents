PHASE: PR_CREATION — Create a draft MR on GitLab and hand off to reviewers.

Do NOT create the MR yourself. Instruct the Developer teammate (still alive — do NOT spawn a new one).

CHECKLIST:
- [ ] Step 1 — Extract context for MR description:
      a) Identify the Jira ticket from the branch name (pattern: RISKDEV-XXXX).
         If present, use the MCP Jira tool to fetch the ticket summary for the MR title.
      b) Gather the commit log:
            git log BASE_BRANCH..HEAD --oneline
      c) Gather the diff stats:
            git diff --stat BASE_BRANCH
      Use this information to populate the MR description template below.

- [ ] Step 2 — Create draft MR:
      Instruct Developer to run:
        glab mr create --draft --target-branch BASE_BRANCH \
          --title "<RISKDEV-XXXX: short description>" \
          --description "<body>"

      MR body template:
      ---
      ## Problem
      <What problem does this MR solve? Link Jira ticket: RISKDEV-XXXX>

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

- [ ] Wait for Developer to report the MR URL
- [ ] Present MR URL to user
- [ ] Transition: {{WF_CLIENT}} transition <id> --to FEEDBACK --reason "MR created: <url>"

VERIFY: Current branch must NOT be BASE_BRANCH. If it is, a feature branch was not created in PLANNING.
If BASE_BRANCH is not main/master, --target-branch is REQUIRED.

BLOCKED ACTIONS: Edit, Write, NotebookEdit, git commit, git push. Only MR commands and teammate communication allowed.
