# wf-web Channel Plugin

Claude Code Channel plugin that enables sending messages from the web dashboard into running Claude Code sessions.

## Prerequisites

- Claude Code v2.1.80+
- Node.js 18+

## How it works

1. Plugin runs as an MCP server spawned by Claude Code (stdio transport)
2. Simultaneously listens on an HTTP port on `127.0.0.1` (localhost only)
3. Writes port to `$TMPDIR/wf-agents-channel-ports/<session-id>.json` for discovery
4. Web dashboard → Go backend (`POST /api/workflows/{id}/message`) → Channel plugin HTTP → `mcp.notification()` → Claude session
5. Messages appear in Claude's context as `<channel source="wf-web">message</channel>`

## Installation

```bash
cd channels/wf-web
npm install
```

## Usage

### Development mode (standalone testing)

```bash
CLAUDE_SESSION_ID=<session-id> npx tsx index.ts
# or:
npx tsx index.ts --session-id <session-id>
```

Plugin starts HTTP server and logs the port. Test with:
```bash
PORT=$(cat $TMPDIR/wf-agents-channel-ports/<session-id>.json | python3 -c "import json,sys; print(json.load(sys.stdin)['port'])")
curl http://127.0.0.1:$PORT/health
curl -X POST http://127.0.0.1:$PORT/message -H 'Content-Type: application/json' -d '{"message":"hello"}'
```

Note: In standalone mode, MCP notifications go to stdout (no Claude Code receiving them). Use this for testing HTTP connectivity only.

### With Claude Code

Register in `.mcp.json`:
```json
{
  "mcpServers": {
    "wf-web": {
      "command": "npx",
      "args": ["tsx", "<path-to>/channels/wf-web/index.ts"]
    }
  }
}
```

Then start Claude Code with channels:
```bash
claude --dangerously-load-development-channels server:wf-web
```

Messages sent from the web dashboard will appear in the session as `<channel>` tags.

## API

### POST /message
Send a message to the Claude session.
- Body: `{ "message": "string" }`
- Response: `{ "status": "delivered" }` (200) or error (400/500)

### GET /health
Health check.
- Response: `{ "status": "ok", "session_id": "...", "port": N }`

## Port discovery

The plugin writes `$TMPDIR/wf-agents-channel-ports/<session-id>.json`:
```json
{ "port": 12345, "session_id": "coding-session-xxx" }
```

The Go web server reads this file to forward messages. File is removed on graceful shutdown (SIGTERM/SIGINT).

## Session ID

Determined in order:
1. `CLAUDE_SESSION_ID` environment variable (set automatically by Claude Code for MCP servers)
2. `--session-id <value>` CLI argument
3. Falls back to `"unknown"` with stderr warning
