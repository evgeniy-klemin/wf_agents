---
name: start-iriski-team
description: "Start an autonomous feature workflow with Team Lead, Developer, and Reviewer agents"
disable-model-invocation: true
argument-hint: "--session <id> --task '<description>'"
---

First, read the file at `${CLAUDE_PLUGIN_ROOT}/agents/iriski-team-lead.md` and assume the role of the iriski-team-lead.

Then initialize the feature team workflow:

```
/wf-agents:workflow start --session ${CLAUDE_SESSION_ID} $ARGUMENTS
```

The workflow hooks will output a checklist. Execute each item in order — do NOT skip ahead to transition.

IMPORTANT: If context is compressed during the session, re-read `${CLAUDE_PLUGIN_ROOT}/agents/iriski-team-lead.md` to recover your full role and protocol.

## Required parameters

- `--task '<description>'` — task description (REQUIRED, workflow will fail without it)
