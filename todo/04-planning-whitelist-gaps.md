---
title: Add missing commands to PLANNING whitelist
status: planned
priority: medium
created: 2026-03-15
---

## Problem

`go version` and `xargs` blocked in PLANNING phase (not in safeBashPrefixes). These are read-only commands that should be allowed. Explore subagent uses `find | xargs grep` which gets denied.

## Solution

Add to `safeBashPrefixes` in `guards.go`:
- `xargs`
- `go version`

Or: move to config-driven whitelist (depends on #01).

## Files to modify

- `internal/workflow/guards.go` — safeBashPrefixes
