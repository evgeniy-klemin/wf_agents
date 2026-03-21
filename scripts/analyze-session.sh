#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
SESSION_ID="${1:?Usage: analyze-session.sh <session-id>}"
WORKFLOW_ID="coding-session-$SESSION_ID"
CACHE_DIR="/tmp/wf-analysis/$SESSION_ID"

mkdir -p "$CACHE_DIR"

# Cache status and timeline once
if [ ! -f "$CACHE_DIR/status.txt" ]; then
  $WF_CLIENT status "$WORKFLOW_ID" > "$CACHE_DIR/status.txt" 2>&1 || { echo "Cannot query $WORKFLOW_ID"; exit 1; }
fi
if [ ! -f "$CACHE_DIR/timeline.json" ]; then
  $WF_CLIENT timeline "$WORKFLOW_ID" > "$CACHE_DIR/timeline.json" 2>&1 || { echo "Cannot get timeline (session too large?)"; touch "$CACHE_DIR/timeline.json"; }
fi

STATUS="$CACHE_DIR/status.txt"
TIMELINE="$CACHE_DIR/timeline.json"

echo "=== STATUS ==="
cat "$STATUS"

echo ""
echo "=== TRANSITIONS ==="
grep -E '"type": "transition"' -A8 "$TIMELINE" | grep -E '"from"|"to"|"reason"|"iteration"' || true

echo ""
echo "=== AGENTS ==="
grep -E 'agent_spawn|agent_stop' -A6 "$TIMELINE" | grep -E 'timestamp|agent_id|agent_type|hook_type' || true

echo ""
echo "=== TEAMMATE IDLE ==="
grep 'TeammateIdle' -A5 "$TIMELINE" | grep 'teammate_name\|team_name' || true

echo ""
echo "=== METRICS ==="
echo "Transitions: $(grep -c '"type": "transition"' "$TIMELINE" || echo 0)"
echo "Auto-BLOCKED: $(grep -c 'auto:' "$TIMELINE" || echo 0)"
echo "Errors: $(grep -c 'PostToolUseFailure' "$TIMELINE" || echo 0)"
echo "Denials: $(grep -c '"denied"' "$TIMELINE" || echo 0)"
echo "SendMessage: $(grep -c '"tool": "SendMessage"' "$TIMELINE" || echo 0)"
echo "TeammateIdle: $(grep -c 'TeammateIdle' "$TIMELINE" || echo 0)"
