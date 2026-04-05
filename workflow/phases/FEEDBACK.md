PHASE: FEEDBACK — Triage human PR/MR review comments with the team.

## CHECKLIST

**Step 1: Transition to BLOCKED**

Immediately upon entering FEEDBACK, transition to BLOCKED with the reason:
"MR review expected. Please review the MR, leave comments, and move it from Draft to Ready when done."

Command:
`{{WF_CLIENT}} transition <id> --to BLOCKED --reason "MR review expected. Please review the MR, leave comments, and move it from Draft to Ready when done." --repo {WORKTREE}`

The agent will auto-return to FEEDBACK when the user responds to the conversation.

**Step 2: Check MR status**

After returning from BLOCKED, run:
`{{PLUGIN_ROOT}}/bin/feedback-poll --repo {WORKTREE}`

Parse the JSON output:
- If `status` is `"approved"` or `"ready"` → go to Step 5
- If `new_inline_comments` or `new_pr_comments` are non-empty → go to Step 3
- If no new comments AND MR still draft → transition to BLOCKED with reason:
  "No new comments detected. MR is still in Draft. Please review and move MR from Draft to Ready when done."
  Then repeat from Step 2 after auto-return.

**Step 3: When new comments found — triage each comment:**

1. **Accept** — implement the change (will loop back through RESPAWN)
2. **Reject** — provide technical reasoning in the PR/MR comment
3. **Escalate** — transition to BLOCKED if user input needed

**Step 4: Reply to every comment explicitly.**

CRITICAL timing rule:
- Accepted comments (changes needed): implement ALL changes first (RESPAWN → DEVELOPING → ... → push),
  return to FEEDBACK, THEN reply to each comment describing what was done and which commit contains
  the fix. Do NOT reply "will do X" or "I'll fix this" before the work is done — the reply must
  describe what WAS done.
- Rejected comments (no changes needed): reply immediately with clear technical reasoning for why
  the suggestion doesn't apply or would be harmful.

Each reply must be:
- Transparent — clearly state what was done or why not
- Concise — short but with enough context so the reviewer understands without checking code
- For accepted comments: describe the change made, which files were affected, and which commit SHA
- For rejected comments: explain the technical reason why the suggestion doesn't apply or would be harmful

Reply commands by platform:

GitHub:
- PR-level reply: `gh pr comment <number> --body "..."`
- Inline reply:   `gh api repos/{owner}/{repo}/pulls/{number}/comments/{id}/replies -f body="..."`

GitLab:
- MR-level reply: `glab mr note <iid> --message "..."`
- Inline reply:   `glab api projects/{id}/merge_requests/{iid}/discussions/{discussion_id}/notes -f body="..."`

1. **Changes needed** → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Implementing feedback: <summary>" --repo {WORKTREE}
      If RESPAWN DENIED due to max iterations, follow the MAX-ITERATIONS PROTOCOL:
      ask user, reset-iterations if yes, proceed to COMPLETE if PR approved/no changes needed.
      After iterating, return to FEEDBACK and repeat from Step 1.

**Step 5: Transition to COMPLETE when approved or ready.**

When status=approved or status=ready:
`{{WF_CLIENT}} transition <id> --to COMPLETE --reason "All PR feedback resolved, PR approved or ready" --repo {WORKTREE}`
GUARD: COMPLETE requires reviewDecision=APPROVED or MR moved from draft to ready. Transition will be DENIED otherwise.

BLOCKED ACTIONS: Edit, Write, NotebookEdit (except during iteration via RESPAWN). Only PR review and teammate communication allowed.
