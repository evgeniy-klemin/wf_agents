#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
SESSION_ID="${1:?Usage: session-summary.sh <session-id>}"
WORKFLOW_ID="coding-session-$SESSION_ID"
CACHE_DIR="/tmp/wf-analysis/$SESSION_ID"

mkdir -p "$CACHE_DIR"

# Cache status and timeline once
if [ ! -f "$CACHE_DIR/status.txt" ]; then
  $WF_CLIENT status "$WORKFLOW_ID" > "$CACHE_DIR/status.txt" 2>&1 || { echo "Cannot query $WORKFLOW_ID"; exit 1; }
fi
if [ ! -f "$CACHE_DIR/timeline.json" ]; then
  $WF_CLIENT timeline "$WORKFLOW_ID" > "$CACHE_DIR/timeline.json" 2>&1 || { echo "Cannot get timeline"; touch "$CACHE_DIR/timeline.json"; }
fi

STATUS=$(cat "$CACHE_DIR/status.txt")
TIMELINE="$CACHE_DIR/timeline.json"

PHASE=$(echo "$STATUS" | grep "Phase:" | awk '{print $2}')
ITER=$(echo "$STATUS" | grep "Iteration:" | awk '{print $2}')
AGENTS=$(echo "$STATUS" | grep "Active Agents:" | sed 's/Active Agents:  //')
EVENTS=$(echo "$STATUS" | grep "Events:" | awk '{print $2}')
TASK=$(echo "$STATUS" | grep "Task:" | sed 's/Task:           //')

TRANSITIONS=$(grep -c '"type": "transition"' "$TIMELINE" || true)
AUTO_BLOCKED=$(grep -c 'auto:' "$TIMELINE" || true)
ERRORS=$(grep -c 'PostToolUseFailure' "$TIMELINE" || true)
DENIALS=$(grep -c '"denied"' "$TIMELINE" || true)
MSGS=$(grep -c '"tool": "SendMessage"' "$TIMELINE" || true)
IDLE=$(grep -c 'TeammateIdle' "$TIMELINE" || true)

echo "$SESSION_ID  phase=$PHASE  iter=$ITER  events=$EVENTS  transitions=$TRANSITIONS  auto-blocked=$AUTO_BLOCKED  errors=$ERRORS  denials=$DENIALS  msgs=$MSGS  idle=$IDLE  agents=$AGENTS"
echo "  task: $TASK"
