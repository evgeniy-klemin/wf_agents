#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
WORKFLOW_ID="coding-session-$1"

echo "=== STATUS ==="
$WF_CLIENT status $WORKFLOW_ID

echo "=== TRANSITIONS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep -E '"type": "transition"' -A8 | grep -E '"from"|"to"|"reason"|"iteration"' || true

echo "=== AGENTS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep -E 'agent_spawn|agent_stop' -A6 | grep -E 'timestamp|agent_id|agent_type|hook_type' || true

echo "=== TEAMMATE IDLE ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep 'TeammateIdle' -A5 | grep 'teammate_name|team_name' || true

echo "=== METRICS ==="
TIMELINE=$($WF_CLIENT timeline $WORKFLOW_ID)
echo "Transitions: $(echo "$TIMELINE" | grep -c '"type": "transition"' || echo 0)"
echo "Auto-BLOCKED: $(echo "$TIMELINE" | grep -c 'auto:' || echo 0)"
echo "Errors: $(echo "$TIMELINE" | grep -c 'PostToolUseFailure' || echo 0)"
echo "Denials: $(echo "$TIMELINE" | grep -c '"denied"' || echo 0)"
echo "SendMessage: $(echo "$TIMELINE" | grep -c '"tool": "SendMessage"' || echo 0)"
echo "TeammateIdle: $(echo "$TIMELINE" | grep -c 'TeammateIdle' || echo 0)"
