---
title: Investigate rapid teammate spawn/kill cycles
status: planned
priority: low
created: 2026-03-15
---

## Problem

Developer-1 spawned 8+ times in a single iteration. Agents spawn, live 2-4 seconds, die (SubagentStop), respawn. This wastes tokens and time.

## Solution

Investigate root cause:
- Is Claude Code killing teammates that can't connect to team?
- Is it a TeamCreate timing issue?
- Is it permission-related (teammate gets denied, dies)?

May need to analyze JSONL hook logs to trace the exact failure chain per spawn.

## Files to modify

- Analysis only — may not require code changes
