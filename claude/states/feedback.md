PHASE: FEEDBACK — Triage human PR review comments.

CHECKLIST:
- [ ] Check for comments: gh pr view --json reviewDecision,reviews,comments,state
- [ ] If NO comments yet — poll in a loop:
      Run "sleep 60" (Bash), then check again. Repeat until comments appear.
      Do NOT stop or go idle — keep polling.
- [ ] When comments found: gh api repos/{owner}/{repo}/pulls/{pr_number}/comments
- [ ] For each comment: Accept (implement) / Reject (reply with reasoning) / Escalate (BLOCKED)
- [ ] Reply to EVERY comment with a transparent, concise response:
      ACCEPTED: what was changed, which files, brief rationale
      REJECTED: technical reasoning why the change is not needed or harmful
      Keep replies short but with enough context for the reviewer to understand without checking the code
- [ ] Changes needed → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Implementing feedback: <summary>"
- [ ] All comments resolved but PR NOT approved/merged → continue polling loop:
      sleep 60, then gh pr view --json reviewDecision,reviews,comments,state
      Watch for: new comments, reviewDecision=APPROVED, or state=MERGED
      If new comments appear — triage them (repeat from checklist start)
- [ ] PR approved/merged → {{WF_CLIENT}} transition <id> --to COMPLETE --reason "All feedback resolved, PR approved/merged"
      GUARD: requires reviewDecision=APPROVED or state=MERGED. Will be DENIED otherwise.

IMPORTANT: Do NOT stop and wait passively. Poll actively using sleep + gh pr view loop.
