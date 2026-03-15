#!/bin/bash
set -euo pipefail

SESSION_ID="${1:?Usage: raw-hooks.sh <session-id>}"
LOGDIR="${TMPDIR:-/tmp}/wf-agents-hook-logs"

# New JSONL format
JSONL_FILE="$LOGDIR/$SESSION_ID.jsonl"
if [ -f "$JSONL_FILE" ]; then
    echo "=== Hook log: $SESSION_ID.jsonl ($(wc -l < "$JSONL_FILE") events) ==="
    # Show summary by event type
    echo "--- Events by type ---"
    cat "$JSONL_FILE" | python3 -c "
import json, sys
from collections import Counter
counts = Counter()
for line in sys.stdin:
    try:
        obj = json.loads(line)
        counts[obj.get('event', '?')] += 1
    except: pass
for event, count in counts.most_common():
    print(f'  {count:4d} {event}')
" 2>/dev/null || cat "$JSONL_FILE" | grep -o '"event":"[^"]*"' | sort | uniq -c | sort -rn

    echo ""
    echo "--- Last 20 events ---"
    tail -20 "$JSONL_FILE" | python3 -c "
import json, sys
for line in sys.stdin:
    try:
        obj = json.loads(line)
        ts = obj.get('ts', '?')[11:19]
        event = obj.get('event', '?')
        raw = obj.get('raw', {})
        agent = raw.get('agent_id', '') or raw.get('teammate_name', '')
        tool = raw.get('tool_name', '')
        extra = agent or tool
        print(f'{ts} {event:20} {extra}')
    except: pass
" 2>/dev/null || tail -20 "$JSONL_FILE"
else
    echo "No JSONL log found for $SESSION_ID"
fi

# Also check legacy per-event format
echo ""
echo "=== Legacy hook logs ==="
found=0
for f in "$LOGDIR"/*-"$SESSION_ID".json; do
    [ -f "$f" ] || continue
    found=1
    echo "--- $(basename "$f") ---"
    python3 -m json.tool "$f" 2>/dev/null || cat "$f"
    echo
done
if [ "$found" -eq 0 ]; then
    echo "No legacy logs found"
fi
