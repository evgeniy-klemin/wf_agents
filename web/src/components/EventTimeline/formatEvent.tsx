import type { ReactElement } from 'react'
import { truncate, shortPath } from '@/lib/formatting'

export function parseInput(rawInput: unknown): Record<string, string> {
  if (!rawInput) return {}
  try {
    return typeof rawInput === 'string' ? JSON.parse(rawInput) : (rawInput as Record<string, string>)
  } catch {
    return {}
  }
}

export function hookDescription(hookType: string, d: Record<string, string>): ReactElement {
  switch (hookType) {
    case 'Notification':
      return <span><span className="text-yellow-400">Notification received</span></span>
    case 'TeammateIdle':
      return (
        <span>
          <span className="text-yellow-400">Waiting for teammate</span>
          {d.agent_type ? ` (${d.agent_type})` : ''}
        </span>
      )
    case 'UserPromptSubmit':
      return (
        <span>
          <span className="text-blue-400">User input</span>
          {d.prompt ? `: ${truncate(d.prompt, 80)}` : ''}
        </span>
      )
    case 'SessionStart':
      return (
        <span>
          <span className="text-green-400">Session started</span>
          {d.model ? <span className="text-gray-500"> {d.model}</span> : null}
        </span>
      )
    case 'Stop':
      return <span className="text-gray-400">Session stopped</span>
    default:
      return <span>{hookType}</span>
  }
}

export function toolHumanDesc(toolName: string, rawInput: unknown): ReactElement {
  const input = parseInput(rawInput)
  switch (toolName) {
    case 'Bash':
      return (
        <span>
          <span className="text-blue-300">Run</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{truncate(input.command || '', 60)}</code>
        </span>
      )
    case 'Read':
      return (
        <span>
          <span className="text-blue-300">Read</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{input.file_path ? shortPath(input.file_path) : 'file'}</code>
        </span>
      )
    case 'Edit':
      return (
        <span>
          <span className="text-yellow-300">Edit</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{input.file_path ? shortPath(input.file_path) : 'file'}</code>
        </span>
      )
    case 'Write':
      return (
        <span>
          <span className="text-yellow-300">Write</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{input.file_path ? shortPath(input.file_path) : 'file'}</code>
        </span>
      )
    case 'Glob':
      return (
        <span>
          <span className="text-blue-300">Find files</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{input.pattern || '*'}</code>
        </span>
      )
    case 'Grep':
      return (
        <span>
          <span className="text-blue-300">Search</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">/{truncate(input.pattern || '', 40)}/</code>
        </span>
      )
    case 'Agent':
      return (
        <span>
          <span className="text-purple-300">Agent</span>{' '}
          {truncate(input.description || 'task', 50)}
        </span>
      )
    case 'WebFetch':
      return (
        <span>
          <span className="text-blue-300">Fetch</span>{' '}
          <code className="text-xs bg-gray-800 px-1 rounded">{truncate(input.url || '', 50)}</code>
        </span>
      )
    case 'WebSearch':
      return (
        <span>
          <span className="text-blue-300">Search web</span>{' '}
          &ldquo;{truncate(input.query || '', 50)}&rdquo;
        </span>
      )
    case 'AskUserQuestion':
      return (
        <span>
          <span className="text-yellow-300">Ask user</span>{' '}
          &ldquo;{truncate(input.question || '', 50)}&rdquo;
        </span>
      )
    case 'Skill':
      return (
        <span>
          <span className="text-purple-300">Skill</span> /{input.skill || '?'}
        </span>
      )
    case 'ToolSearch':
      return (
        <span>
          <span className="text-blue-300">Discover tool</span>{' '}
          {truncate(input.query || '', 40)}
        </span>
      )
    default:
      return <strong>{toolName}</strong>
  }
}

export function formatEventDetail(ev: { type: string; session_id?: string; detail?: Record<string, string> }): ReactElement {
  const d = ev.detail || {}
  const hookType = d.hook_type || ''

  switch (ev.type) {
    case 'transition': {
      const from = d.from || '?'
      const to = d.to || '?'
      if (to === 'BLOCKED') {
        return (
          <span>
            <span className="text-red-400">⏸ Paused</span>{' '}
            <span className="text-gray-500">from {from}</span>
            {d.reason ? <span className="text-gray-600"> — {d.reason}</span> : null}
          </span>
        )
      }
      if (from === 'BLOCKED') {
        return (
          <span>
            <span className="text-green-400">▶ Resumed</span> back to {to}
            {d.reason ? <span className="text-gray-500"> — {d.reason}</span> : null}
          </span>
        )
      }
      return (
        <span>
          {from} → {to}
          {d.reason ? <span className="text-gray-500"> — {d.reason}</span> : null}
        </span>
      )
    }
    case 'tool_use': {
      const tool = d.tool_name || d.tool || ''
      if (!tool && hookType) return hookDescription(hookType, d)
      if (hookType === 'PostToolUse' || hookType === 'PostToolUseFailure') {
        return (
          <span>
            {toolHumanDesc(tool, d.tool_input)}
            {d.error ? <span className="text-red-400"> ✖ {truncate(d.error, 80)}</span> : null}
            {hookType === 'PostToolUseFailure'
              ? <span className="text-red-400"> [failed]</span>
              : <span className="text-green-500"> ✔</span>}
          </span>
        )
      }
      return (
        <span>
          {toolHumanDesc(tool, d.tool_input)}
          {d.denied === 'true' ? <span className="text-red-400"> [denied]</span> : null}
          {d.denied === 'true' && d.reason ? <span className="text-gray-500"> — {d.reason}</span> : null}
        </span>
      )
    }
    case 'hook_denial': {
      const from = d.from || ''
      const to = d.to || ''
      if (from && to) {
        return (
          <span>
            <span className="text-red-400">Transition denied:</span>{' '}
            {from} → {to}{' '}
            <span className="text-gray-500">— {d.reason || ''}</span>
          </span>
        )
      }
      const htool = d.tool_name || d.tool || ''
      return (
        <span>
          {htool ? toolHumanDesc(htool, d.tool_input) : 'Action'}{' '}
          <span className="text-red-400">[denied]</span>{' '}
          <span className="text-gray-500">{d.reason || ''}</span>
        </span>
      )
    }
    case 'agent_spawn':
      return <span>Agent started: <strong>{d.agent_type || d.agent_id || '?'}</strong></span>
    case 'agent_stop':
      return <span>Agent stopped: <strong>{d.agent_type || d.agent_id || '?'}</strong></span>
    case 'journal':
      if (ev.session_id === 'web-dashboard') {
        return (
          <span>
            <span className="text-blue-400 text-xs font-medium px-1 py-0.5 bg-blue-900/30 rounded mr-1">Web</span>
            {d.message || ''}
          </span>
        )
      }
      return <span>{d.message || ''}</span>
    case 'error':
      return <span className="text-red-400">{d.error || d.message || 'Unknown error'}</span>
    default:
      return hookType
        ? hookDescription(hookType, d)
        : <span>{JSON.stringify(d).substring(0, 200)}</span>
  }
}


