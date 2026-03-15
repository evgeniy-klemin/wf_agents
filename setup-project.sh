#!/bin/bash
# Setup a target project for Temporal-managed Claude Code workflow.
#
# Usage: ./setup-project.sh /path/to/target/project
#
set -euo pipefail

WF_AGENTS_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="${WF_AGENTS_DIR}/bin"

if [ $# -lt 1 ]; then
    echo "Usage: $0 <target-project-path>"
    echo ""
    echo "This will add workflow hooks and agent role configs to your project."
    echo ""
    echo "Files created:"
    echo "  <project>/.claude/settings.local.json       (hooks config)"
    echo "  <project>/.claude/agents/developer.md        (Developer role rules)"
    echo "  <project>/.claude/agents/reviewer.md         (Reviewer role rules — customize this!)"
    exit 1
fi

TARGET="$1"

if [ ! -d "$TARGET" ]; then
    echo "Error: $TARGET is not a directory"
    exit 1
fi

# Check binaries exist
if [ ! -f "${BIN_DIR}/hook-handler" ]; then
    echo "Error: hook-handler not found. Run 'make build' first."
    exit 1
fi

echo "Setting up Temporal workflow hooks in: $TARGET"

# Create directories
mkdir -p "${TARGET}/.claude/agents"

# Generate settings.local.json with correct bin path, preserving existing permissions
SETTINGS_DEST="${TARGET}/.claude/settings.local.json"
SETTINGS_TEMPLATE=$(sed "s|${WF_AGENTS_DIR}/bin|${BIN_DIR}|g" "${WF_AGENTS_DIR}/hooks/settings.json.example")

if [ -f "$SETTINGS_DEST" ]; then
    # Merge: take hooks from template, keep everything else from existing file
    if command -v jq &>/dev/null; then
        jq -s '.[0] * .[1]' "$SETTINGS_DEST" <(echo "$SETTINGS_TEMPLATE") > "${SETTINGS_DEST}.tmp" \
            && mv "${SETTINGS_DEST}.tmp" "$SETTINGS_DEST"
        echo "  Updated .claude/settings.local.json (hooks updated, existing permissions preserved)"
    else
        echo "  WARNING: jq not found — cannot merge settings.local.json safely"
        echo "  Existing file kept. Manually add hooks from: hooks/settings.json.example"
    fi
else
    echo "$SETTINGS_TEMPLATE" > "$SETTINGS_DEST"
    echo "  Created .claude/settings.local.json"
fi

# Copy agent role configs (only if they don't exist — don't overwrite customizations)
for role in developer reviewer; do
    dest="${TARGET}/.claude/agents/${role}.md"
    if [ -f "$dest" ]; then
        echo "  .claude/agents/${role}.md already exists — keeping your version"
    else
        cp "${WF_AGENTS_DIR}/agents/${role}.md" "$dest"
        echo "  Created .claude/agents/${role}.md"
    fi
done

# Add local files to .gitignore
GITIGNORE_ENTRIES=".claude/settings.local.json"
if [ -f "${TARGET}/.gitignore" ]; then
    if ! grep -q "settings.local.json" "${TARGET}/.gitignore"; then
        echo "$GITIGNORE_ENTRIES" >> "${TARGET}/.gitignore"
        echo "  Added settings.local.json to .gitignore"
    fi
else
    echo "$GITIGNORE_ENTRIES" > "${TARGET}/.gitignore"
    echo "  Created .gitignore"
fi

echo ""
echo "Done! To customize agent behavior, edit:"
echo "  ${TARGET}/.claude/agents/reviewer.md       — review criteria, when to approve/reject"
echo "  ${TARGET}/.claude/agents/developer.md      — development approach, TDD rules"
echo ""
echo "To start:"
echo "  1. Temporal running:  cd ${WF_AGENTS_DIR} && docker compose up -d"
echo "  2. Worker running:    cd ${WF_AGENTS_DIR} && ./bin/worker"
echo "  3. Claude Code:       cd ${TARGET} && claude --plugin-dir ${WF_AGENTS_DIR}"
echo ""
echo "  Monitor at: http://localhost:8080"
