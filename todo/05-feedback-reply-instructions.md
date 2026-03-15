---
title: Improve FEEDBACK phase instructions for already-committed fixes
status: planned
priority: medium
created: 2026-03-15
---

## Problem

In session 75683c29, Team Lead went FEEDBACK → RESPAWN for another iteration even though fixes were already committed. This caused a "pass-through" anti-pattern: empty DEVELOPING → REVIEWING blocked (no changes) → BLOCKED → fake changes to pass guard.

## Solution

Update `states/feedback.md`:
- "If changes from previous iteration already address the comment, stay in FEEDBACK and reply — do NOT start a new RESPAWN iteration"
- "Only RESPAWN if NEW code changes are needed"

## Files to modify

- `states/feedback.md`
