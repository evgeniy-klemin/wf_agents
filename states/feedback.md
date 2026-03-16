PHASE: FEEDBACK — Triage human PR review comments with the team.

CHECKLIST:

**Step 1: Initialize comment tracking**
- [ ] Initialize SEEN_COMMENT_IDS as an empty set. Used to track which comments have already been
      processed (by ID), so new comments are reliably detected across every poll cycle.

**Step 2: Poll loop — ALL THREE checks are MANDATORY on every cycle**

Do NOT stop and wait. Run a continuous polling loop. Each cycle must execute ALL three checks
— do NOT skip any:

- [ ] sleep 60 (Bash)

2a. Check approval and merge status:
- [ ] gh pr view --json reviewDecision,state
      If reviewDecision=APPROVED or state=MERGED → go to Step 5.

2b. Fetch ALL inline review comments and filter out already-seen ones:
- [ ] gh api repos/{owner}/{repo}/pulls/{number}/comments \
        --jq '[.[] | {id, path, line, body, in_reply_to_id, created_at}]'
      Compare returned IDs against SEEN_COMMENT_IDS. Any new IDs = new comments.

2c. Fetch ALL PR-level comments and filter out already-seen ones:
- [ ] gh pr view --json comments --jq '[.comments[] | {id: .id, body: .body, createdAt: .createdAt}]'
      Compare returned IDs against SEEN_COMMENT_IDS. Any new IDs = new comments.

Add new comment IDs to SEEN_COMMENT_IDS after each cycle.
If 2b or 2c returned new comments → go to Step 3. Otherwise → repeat from sleep 60.

WARNING: gh pr view --json comments (2c) only returns PR-level comments, NOT inline code review
comments or thread replies. Step 2b is MANDATORY — without it you will miss all inline review feedback.

**Step 3: When new comments found — triage each comment:**
- [ ] Accept — implement the change (will loop back through RESPAWN)
- [ ] Reject — provide technical reasoning in the PR comment
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

- [ ] Changes needed → {{WF_CLIENT}} transition <id> --to RESPAWN --reason "Implementing feedback: <summary>"
      If RESPAWN DENIED due to max iterations, follow the MAX-ITERATIONS PROTOCOL:
      ask user, reset-iterations if yes, proceed to COMPLETE if PR approved/no changes needed.
      After iterating, return to FEEDBACK and resume the poll loop from Step 2.

**Step 5: Transition to COMPLETE when approved/merged.**
- [ ] From the poll loop, when reviewDecision=APPROVED or state=MERGED:
      {{WF_CLIENT}} transition <id> --to COMPLETE --reason "All PR feedback resolved, PR approved/merged"
      GUARD: COMPLETE requires reviewDecision=APPROVED or state=MERGED. Transition will be DENIED otherwise.

IMPORTANT: Do NOT stop and wait passively. Poll actively using sleep + gh pr view loop.

BLOCKED ACTIONS: Edit, Write, NotebookEdit (except during iteration via RESPAWN). Only PR review and teammate communication allowed.
