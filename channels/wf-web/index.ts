import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import * as http from 'http';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

// Session ID discovery
function getSessionId(): string {
  if (process.env.CLAUDE_SESSION_ID) {
    return process.env.CLAUDE_SESSION_ID;
  }
  const args = process.argv;
  const idx = args.indexOf('--session-id');
  if (idx !== -1 && idx + 1 < args.length) {
    return args[idx + 1];
  }
  console.error('[wf-web] WARNING: no session ID found, using "unknown"');
  return 'unknown';
}

const sessionId = getSessionId();

// MCP server setup
const mcp = new Server(
  { name: 'wf-web', version: '0.1.0' },
  {
    capabilities: {
      experimental: { 'claude/channel': {} },
      tools: {},
    },
    instructions:
      'Messages from the web dashboard arrive as <channel source="wf-web">. These are from the user via the web UI — treat them as user instructions.',
  }
);

const transport = new StdioServerTransport();
await mcp.connect(transport);

// Port file helpers
const portDir = path.join(os.tmpdir(), 'wf-agents-channel-ports');
const portFile = path.join(portDir, `${sessionId}.json`);

function registerPortFile(port: number): void {
  fs.mkdirSync(portDir, { recursive: true });
  fs.writeFileSync(portFile, JSON.stringify({ port, session_id: sessionId }));
}

function removePortFile(): void {
  try {
    fs.unlinkSync(portFile);
  } catch {
    // ignore if already gone
  }
}

// HTTP server
function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on('data', (chunk: Buffer) => chunks.push(chunk));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    req.on('error', reject);
  });
}

const httpServer = http.createServer(async (req, res) => {
  res.setHeader('Content-Type', 'application/json');

  if (req.method === 'POST' && req.url === '/message') {
    let body: string;
    try {
      body = await readBody(req);
    } catch (err) {
      res.writeHead(400);
      res.end(JSON.stringify({ error: 'failed to read request body' }));
      return;
    }

    let parsed: { message?: string };
    try {
      parsed = JSON.parse(body);
    } catch {
      res.writeHead(400);
      res.end(JSON.stringify({ error: 'invalid JSON' }));
      return;
    }

    if (typeof parsed.message !== 'string') {
      res.writeHead(400);
      res.end(JSON.stringify({ error: 'missing "message" field' }));
      return;
    }

    try {
      await mcp.notification({
        method: 'notifications/claude/channel',
        params: {
          content: parsed.message,
          meta: { sender: 'web-dashboard' },
        },
      });
      res.writeHead(200);
      res.end(JSON.stringify({ status: 'delivered' }));
    } catch (err) {
      res.writeHead(500);
      res.end(JSON.stringify({ error: 'failed to send notification' }));
    }
    return;
  }

  if (req.method === 'GET' && req.url === '/health') {
    const addr = httpServer.address() as { port: number } | null;
    res.writeHead(200);
    res.end(JSON.stringify({ status: 'ok', session_id: sessionId, port: addr?.port ?? 0 }));
    return;
  }

  res.writeHead(404);
  res.end(JSON.stringify({ error: 'not found' }));
});

httpServer.listen(0, '127.0.0.1', () => {
  const addr = httpServer.address() as { port: number };
  registerPortFile(addr.port);
  console.error(`[wf-web] HTTP listening on 127.0.0.1:${addr.port} (session=${sessionId})`);
});

// Graceful shutdown
function shutdown(): void {
  removePortFile();
  httpServer.close(() => {
    process.exit(0);
  });
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
