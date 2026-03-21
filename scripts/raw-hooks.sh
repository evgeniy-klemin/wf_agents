#!/bin/bash
set -euo pipefail

SESSION_ID="${1:?Usage: raw-hooks.sh <session-id>}"
LOGDIR="${TMPDIR:-/tmp}/wf-agents-hook-logs"

# JSONL format (per-session, request+response pairs)
JSONL_FILE="$LOGDIR/$SESSION_ID.jsonl"
if [ -f "$JSONL_FILE" ]; then
    TOTAL=$(wc -l < "$JSONL_FILE")
    REQUESTS=$(grep -c '"direction":"request"' "$JSONL_FILE" || true)
    RESPONSES=$(grep -c '"direction":"response"' "$JSONL_FILE" || true)
    echo "=== Hook log: $SESSION_ID.jsonl ($TOTAL lines: $REQUESTS requests, $RESPONSES responses) ==="

    echo ""
    echo "--- Requests by event type ---"
    grep '"direction":"request"' "$JSONL_FILE" | python3 -c "
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
" 2>/dev/null || grep '"direction":"request"' "$JSONL_FILE" | grep -o '"event":"[^"]*"' | sort | uniq -c | sort -rn

    echo ""
    echo "--- Responses by decision ---"
    grep '"direction":"response"' "$JSONL_FILE" | python3 -c "
import json, sys
from collections import Counter
counts = Counter()
for line in sys.stdin:
    try:
        obj = json.loads(line)
        resp = obj.get('response', {})
        decision = resp.get('decision', resp.get('action', '?'))
        exit_code = obj.get('exit_code', 0)
        key = f'{decision} (exit {exit_code})'
        counts[key] += 1
    except: pass
for key, count in counts.most_common():
    print(f'  {count:4d} {key}')
" 2>/dev/null || grep '"direction":"response"' "$JSONL_FILE" | grep -o '"exit_code":[0-9]*' | sort | uniq -c | sort -rn

    echo ""
    echo "--- Denials ---"
    grep '"direction":"response"' "$JSONL_FILE" | python3 -c "
import json, sys
for line in sys.stdin:
    try:
        obj = json.loads(line)
        resp = obj.get('response', {})
        if resp.get('decision') == 'deny' or obj.get('exit_code') == 2:
            ts = obj.get('ts', '?')[11:19]
            event = obj.get('event', '?')
            reason = resp.get('reason', resp.get('action', '?'))
            print(f'  {ts} {event:20} {reason}')
    except: pass
" 2>/dev/null || true

    echo ""
    echo "--- Last 30 events (request+response interleaved) ---"
    tail -30 "$JSONL_FILE" | python3 -c "
import json, sys
for line in sys.stdin:
    try:
        obj = json.loads(line)
        ts = obj.get('ts', '?')[11:19]
        event = obj.get('event', '?')
        direction = obj.get('direction', '?')
        if direction == 'request':
            raw = obj.get('raw', {})
            agent = raw.get('agent_id', '') or raw.get('teammate_name', '')
            tool = raw.get('tool_name', '')
            perm = raw.get('permission_mode', '')
            extra = agent or tool
            if perm and perm != 'default':
                extra = f'{extra} [{perm}]' if extra else f'[{perm}]'
            print(f'{ts} REQ  {event:20} {extra}')
        else:
            resp = obj.get('response', {})
            exit_code = obj.get('exit_code', 0)
            decision = resp.get('decision', resp.get('action', ''))
            reason = resp.get('reason', '')
            info = decision
            if reason:
                info = f'{decision}: {reason[:60]}'
            print(f'{ts} RESP {event:20} exit={exit_code} {info}')
    except: pass
" 2>/dev/null || tail -30 "$JSONL_FILE"
else
    echo "No JSONL log found for $SESSION_ID"
fi
