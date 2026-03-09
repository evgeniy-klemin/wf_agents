# RESPAWN Phase

You are in the RESPAWN phase. This is the iteration boundary — clear context and prepare fresh agents.

## Steps

1. Stop any existing Developer and Reviewer subagents
2. Review the current iteration plan and any feedback from prior iterations
3. Prepare the context for the next iteration (plan + specific task)
4. Spawn fresh Developer and Reviewer subagents with clean context

## Purpose

RESPAWN deliberately clears accumulated context window noise. Each iteration starts with agents that only know:
- The overall plan
- The current iteration's specific task
- Feedback from prior rejections (if any)

## Constraints

- Do NOT write or edit any files
- Do NOT carry over the full conversation history to new agents
- Keep the context passed to new agents minimal and focused

## Output

When agents are ready, transition to DEVELOPING.
