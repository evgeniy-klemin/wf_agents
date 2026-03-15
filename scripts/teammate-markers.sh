#!/bin/bash
set -euo pipefail

MARKERDIR="${TMPDIR:-/tmp}/wf-agents-sessions"

if [ $# -eq 0 ]; then
    echo "=== All markers ==="
    for f in "$MARKERDIR"/*; do
        [ -f "$f" ] || continue
        name=$(basename "$f")
        content=$(cat "$f")
        # Detect type
        if echo "$content" | python3 -c "import json,sys; d=json.load(sys.stdin); print('TEAMMATE' if d.get('parent') else 'LEAD')" 2>/dev/null; then
            type=$( echo "$content" | python3 -c "import json,sys; d=json.load(sys.stdin); print('TEAMMATE (parent: '+d.get('parent','')+')' if d.get('parent') else 'LEAD')" 2>/dev/null || echo "UNKNOWN" )
        else
            type="LEGACY"
        fi
        mod=$(stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S' "$f" 2>/dev/null || stat -c '%y' "$f" 2>/dev/null | cut -d. -f1 || echo "?")
        echo "$name  [$type]  mod=$mod"
    done
else
    SESSION_ID="$1"
    echo "=== Markers for $SESSION_ID ==="
    for f in "$MARKERDIR"/*; do
        [ -f "$f" ] || continue
        content=$(cat "$f")
        if echo "$content" | grep -q "$SESSION_ID"; then
            echo "$(basename "$f"): $content"
        fi
    done
fi
