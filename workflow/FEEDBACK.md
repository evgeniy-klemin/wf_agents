PHASE: FEEDBACK — Triage human PR/MR review comments with the team.

CHECKLIST:

**Step 2: Poll loop — run continuously**

CRITICAL POLLING RULES:
- `sleep 60` MUST be run in FOREGROUND (normal Bash call, NOT run_in_background)
- The sleep blocks your turn — this is intentional. You MUST NOT go idle between polls.
- Do NOT use `run_in_background: true` for sleep — this causes you to attempt idle, which is DENIED in FEEDBACK.
- After each sleep completes, immediately run feedback-poll. No idle attempts between steps.

Loop pattern (repeat until approved/merged):
1. `sleep 60` ← foreground, blocks your turn
2. `{{PLUGIN_ROOT}}/bin/feedback-poll` ← check for new comments/approval
3. Parse the JSON output:
   - If `status` is `"approved"` or `"merged"` → go to Step 5
   - If `new_inline_comments` or `new_pr_comments` are non-empty → go to Step 3
   - Otherwise → go back to step 1

**Step 3: When new comments found — triage each comment:**
- [ ] Accept — implement the change (will loop back through RESPAWN)
- [ ] Reject — provide technical reasoning in the PR/MR comment
- [ ] Escalate — transition to BLOCKED if user input needed

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

- [ ] Changes needed → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Implementing feedback: <summary>"
      If RESPAWN DENIED due to max iterations, follow the MAX-ITERATIONS PROTOCOL:
      ask user, reset-iterations if yes, proceed to COMPLETE if PR approved/no changes needed.
      After iterating, return to FEEDBACK and resume the poll loop from Step 2.

**Step 5: Transition to COMPLETE when approved/merged.**
- [ ] From the poll loop, when status=approved or status=merged:
      {{WF_CLIENT}} transition <id> --to COMPLETE --reason "All PR feedback resolved, PR approved/merged"
      GUARD: COMPLETE requires reviewDecision=APPROVED or state=MERGED. Transition will be DENIED otherwise.

IMPORTANT: Do NOT stop and wait passively. Poll actively using sleep + feedback-poll loop.

BLOCKED ACTIONS: Edit, Write, NotebookEdit (except during iteration via RESPAWN). Only PR review and teammate communication allowed.
