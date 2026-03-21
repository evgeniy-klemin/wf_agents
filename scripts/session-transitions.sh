#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
SESSION_ID="${1:?Usage: session-transitions.sh <session-id>}"
WORKFLOW_ID="coding-session-$SESSION_ID"
CACHE_DIR="/tmp/wf-analysis/$SESSION_ID"

mkdir -p "$CACHE_DIR"

if [ ! -f "$CACHE_DIR/timeline.json" ]; then
  $WF_CLIENT timeline "$WORKFLOW_ID" > "$CACHE_DIR/timeline.json" 2>&1 || { echo "Cannot get timeline"; exit 1; }
fi

TIMELINE="$CACHE_DIR/timeline.json"

grep -E '"type": "transition"' -A8 "$TIMELINE" | grep -E '"from"|"to"|"reason"|"iteration"|timestamp' || true
echo "---"
echo "Auto-BLOCKED count: $(grep -c 'auto:' "$TIMELINE" || echo 0)"
