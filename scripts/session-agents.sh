#!/bin/bash
set -euo pipefail

WF_CLIENT="$(dirname "$0")/../bin/wf-client"
WORKFLOW_ID="coding-session-$1"

echo "=== ACTIVE AGENTS ==="
$WF_CLIENT status $WORKFLOW_ID | grep "Active Agents" || true

echo "=== AGENT SPAWNS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep 'agent_spawn' -A6 | grep 'timestamp\|agent_id\|agent_type' || true

echo "=== AGENT STOPS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep 'agent_stop' -A8 | grep 'timestamp\|agent_id\|agent_type\|hook_type' || true

echo "=== TEAMMATE IDLE ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep 'TeammateIdle' -A5 | grep 'teammate_name\|team_name\|timestamp' || true

echo "=== AGENT TYPE COUNTS ==="
$WF_CLIENT timeline $WORKFLOW_ID | grep '"agent_type"' | grep -v tool_input | sort | uniq -c | sort -rn || true
