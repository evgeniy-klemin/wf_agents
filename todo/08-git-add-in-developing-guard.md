---
title: Clarify git restrictions in developer instructions
status: planned
priority: low
created: 2026-03-15
---

## Problem

Developer tried `git add` in DEVELOPING (denied by guard). Instructions should make this clearer — no git operations except in COMMITTING.

## Solution

Update `agents/developer.md`:
- Add explicit note: "Do NOT run git add, git commit, or git push. Leave all changes uncommitted. The Team Lead handles git operations in COMMITTING phase."

## Files to modify

- `agents/developer.md`
