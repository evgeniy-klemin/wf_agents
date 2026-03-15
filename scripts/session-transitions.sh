#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
WORKFLOW_ID="coding-session-$1"

$WF_CLIENT timeline $WORKFLOW_ID | grep -E '"type": "transition"' -A8 | grep -E '"from"|"to"|"reason"|"iteration"|timestamp' || true
echo "---"
echo "Auto-BLOCKED count: $($WF_CLIENT timeline $WORKFLOW_ID | grep -c 'auto:' || echo 0)"
