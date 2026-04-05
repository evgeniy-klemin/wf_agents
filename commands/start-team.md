---
name: start-team
description: "Start an autonomous feature workflow with Team Lead, Developer, and Reviewer agents"
disable-model-invocation: true
argument-hint: "--session <id> --task '<short English title>'"
---

Run: `${CLAUDE_PLUGIN_ROOT}/bin/wf-client lead-protocol`
The output contains the Team Lead protocol — assume that role.

Then initialize the feature team workflow:

## Detecting --repo path

- Check the CLAUDE.md path in your system context:
  - If it contains `.claude/worktrees/<name>/CLAUDE.md`: you are in a worktree.
    Extract the worktree path from the CLAUDE.md path (everything up to and including `<name>`).
    Use that extracted path as `--repo`. Do NOT use `$(pwd)` — it always resolves to the main repo root.
  - Otherwise: you are in the main repo. `--repo $(pwd)` is correct.

```
/wf-agents:workflow start --session ${CLAUDE_SESSION_ID} --repo <worktree-or-pwd> $ARGUMENTS
```

The workflow hooks will output a checklist. Execute each item in order — do NOT skip ahead to transition.

IMPORTANT: If context is compressed during the session, re-run `${CLAUDE_PLUGIN_ROOT}/bin/wf-client lead-protocol` — the output contains your full role and protocol.

## Required parameters

- `--task '<title>'` — short English description (2-5 words, max 60 chars). This is displayed in the web dashboard sidebar. (REQUIRED)
