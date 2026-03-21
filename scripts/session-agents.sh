#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
SESSION_ID="${1:?Usage: session-agents.sh <session-id>}"
WORKFLOW_ID="coding-session-$SESSION_ID"
CACHE_DIR="/tmp/wf-analysis/$SESSION_ID"

mkdir -p "$CACHE_DIR"

if [ ! -f "$CACHE_DIR/status.txt" ]; then
  $WF_CLIENT status "$WORKFLOW_ID" > "$CACHE_DIR/status.txt" 2>&1 || { echo "Cannot query $WORKFLOW_ID"; exit 1; }
fi
if [ ! -f "$CACHE_DIR/timeline.json" ]; then
  $WF_CLIENT timeline "$WORKFLOW_ID" > "$CACHE_DIR/timeline.json" 2>&1 || { echo "Cannot get timeline"; exit 1; }
fi

TIMELINE="$CACHE_DIR/timeline.json"

echo "=== ACTIVE AGENTS ==="
grep "Active Agents" "$CACHE_DIR/status.txt" || true

echo "=== AGENT SPAWNS ==="
grep 'agent_spawn' -A6 "$TIMELINE" | grep 'timestamp\|agent_id\|agent_type' || true

echo "=== AGENT STOPS ==="
grep 'agent_stop' -A8 "$TIMELINE" | grep 'timestamp\|agent_id\|agent_type\|hook_type' || true

echo "=== TEAMMATE IDLE ==="
grep 'TeammateIdle' -A5 "$TIMELINE" | grep 'teammate_name\|team_name\|timestamp' || true

echo "=== AGENT TYPE COUNTS ==="
grep '"agent_type"' "$TIMELINE" | grep -v tool_input | sort | uniq -c | sort -rn || true
