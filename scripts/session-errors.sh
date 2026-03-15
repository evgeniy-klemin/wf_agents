#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
WORKFLOW_ID="coding-session-$1"

echo "=== ERRORS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep -B1 -A8 'PostToolUseFailure' | grep '"error"\|"tool"\|timestamp' || true

echo "=== DENIALS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep -B2 -A5 '"denied": "true"' | grep '"reason"\|"tool"\|timestamp' || true
