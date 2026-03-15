---
title: Handle TeamCreate errors gracefully
status: planned
priority: medium
created: 2026-03-15
---

## Problem

Two errors occur with TeamCreate:
1. "Team does not exist. Call spawnTeam first." — Agent called before TeamCreate
2. "Already leading team. Use TeamDelete first." — TeamCreate called twice

## Solution

Update instructions in `states/developing.md` and `agents/feature-team-lead.md`:
- TeamCreate only on first iteration
- If "Already leading" error → skip (team exists)
- If "does not exist" error → call TeamCreate first, then retry Agent

## Files to modify

- `states/developing.md`
- `agents/feature-team-lead.md`
