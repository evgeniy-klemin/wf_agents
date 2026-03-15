#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
WORKFLOW_ID="coding-session-${1:?Usage: session-summary.sh <session-id>}"

STATUS=$($WF_CLIENT status "$WORKFLOW_ID" 2>/dev/null) || { echo "Cannot query $WORKFLOW_ID"; exit 1; }
TIMELINE=$($WF_CLIENT timeline "$WORKFLOW_ID" 2>/dev/null) || { echo "Cannot get timeline"; exit 1; }

PHASE=$(echo "$STATUS" | grep "Phase:" | awk '{print $2}')
ITER=$(echo "$STATUS" | grep "Iteration:" | awk '{print $2}')
AGENTS=$(echo "$STATUS" | grep "Active Agents:" | sed 's/Active Agents:  //')
EVENTS=$(echo "$STATUS" | grep "Events:" | awk '{print $2}')
TASK=$(echo "$STATUS" | grep "Task:" | sed 's/Task:           //')

TRANSITIONS=$(echo "$TIMELINE" | grep -c '"type": "transition"' || true)
AUTO_BLOCKED=$(echo "$TIMELINE" | grep -c 'auto:' || true)
ERRORS=$(echo "$TIMELINE" | grep -c 'PostToolUseFailure' || true)
DENIALS=$(echo "$TIMELINE" | grep -c '"denied"' || true)
MSGS=$(echo "$TIMELINE" | grep -c '"tool": "SendMessage"' || true)
IDLE=$(echo "$TIMELINE" | grep -c 'TeammateIdle' || true)

echo "$1  phase=$PHASE  iter=$ITER  events=$EVENTS  transitions=$TRANSITIONS  auto-blocked=$AUTO_BLOCKED  errors=$ERRORS  denials=$DENIALS  msgs=$MSGS  idle=$IDLE  agents=$AGENTS"
echo "  task: $TASK"
