import { useState, useEffect, useCallback } from 'react'
import './styles/globals.css'
import { useWorkflowConfig } from '@/hooks/useWorkflowConfig'
import { useWorkflows } from '@/hooks/useWorkflows'
import { useWorkflowDetail } from '@/hooks/useWorkflowDetail'
import { useNow } from '@/hooks/useNow'
import { useNotifications } from '@/hooks/useNotifications'
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts'
import { getGlobalPriorityStatus } from '@/lib/stuck'
import { statusColor } from '@/lib/formatting'
import Header from '@/components/Header'
import Sidebar from '@/components/Sidebar'
import WorkflowView from '@/components/WorkflowView'

export default function App() {
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(() => {
    const params = new URLSearchParams(window.location.search)
    return params.get('session')
  })
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(() => {
    return localStorage.getItem('wf-sidebar-collapsed') === 'true'
  })
  const [showAgents, setShowAgents] = useState(true)
  const [showShortcutsHelp, setShowShortcutsHelp] = useState(false)

  const now = useNow(5000)
  const configResult = useWorkflowConfig()
  const { workflows, isConnected, refresh } = useWorkflows()
  const { status, timeline, config } = useWorkflowDetail(
    selectedWorkflowId,
    configResult.isLoaded ? configResult : null,
  )

  const isRunning = status?.phase !== 'COMPLETE' && status?.phase !== 'BLOCKED'

  const notificationState = useNotifications(selectedWorkflowId, status ?? null, !!isRunning, now)

  // Set dark mode
  useEffect(() => {
    document.documentElement.classList.add('dark')
  }, [])

  // Persist sidebar collapsed state
  useEffect(() => {
    localStorage.setItem('wf-sidebar-collapsed', String(sidebarCollapsed))
  }, [sidebarCollapsed])

  // Dynamic document.title
  useEffect(() => {
    const { phase, isStuck } = getGlobalPriorityStatus(workflows)
    if (phase === 'BLOCKED') {
      document.title = '⏸ BLOCKED — Workflow Dashboard'
    } else if (isStuck) {
      document.title = '⚠️ Stuck — Workflow Dashboard'
    } else if (phase !== null) {
      document.title = '▶ Working — Workflow Dashboard'
    } else {
      document.title = 'Workflow Dashboard'
    }
  }, [workflows, now])

  // Dynamic favicon
  useEffect(() => {
    const { phase, isStuck } = getGlobalPriorityStatus(workflows)

    const color = statusColor(phase, isStuck)

    const canvas = document.createElement('canvas')
    canvas.width = 32
    canvas.height = 32
    const ctx = canvas.getContext('2d')
    if (ctx) {
      ctx.beginPath()
      ctx.arc(16, 16, 14, 0, Math.PI * 2)
      ctx.fillStyle = color
      ctx.fill()
    }

    const dataUrl = canvas.toDataURL('image/png')
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) {
      link = document.createElement('link')
      link.rel = 'icon'
      document.head.appendChild(link)
    }
    link.href = dataUrl
  }, [workflows, now])

  function handleSelect(workflowId: string, runId: string) {
    setSelectedWorkflowId(workflowId)
    setSelectedRunId(runId)
    const params = new URLSearchParams(window.location.search)
    params.set('session', workflowId)
    window.history.replaceState(null, '', `?${params.toString()}`)
  }

  function handleHome() {
    setSelectedWorkflowId(null)
    setSelectedRunId(null)
    const params = new URLSearchParams(window.location.search)
    params.delete('session')
    const qs = params.toString()
    window.history.replaceState(null, '', qs ? `?${qs}` : window.location.pathname)
  }

  const navigateWorkflow = useCallback((direction: 'next' | 'prev') => {
    if (workflows.length === 0) return
    const idx = workflows.findIndex(w => w.workflow_id === selectedWorkflowId)
    let next: number
    if (direction === 'next') {
      next = idx < 0 ? 0 : (idx + 1) % workflows.length
    } else {
      next = idx <= 0 ? workflows.length - 1 : idx - 1
    }
    const w = workflows[next]
    handleSelect(w.workflow_id, w.run_id)
  }, [workflows, selectedWorkflowId])

  const keyboardHandlers = useCallback(() => ({
    j: () => navigateWorkflow('next'),
    k: () => navigateWorkflow('prev'),
    h: () => handleHome(),
    r: () => refresh(),
    a: () => setShowAgents(v => !v),
    s: () => setSidebarCollapsed(v => !v),
    '?': () => setShowShortcutsHelp(v => !v),
  }), [navigateWorkflow, refresh])

  useKeyboardShortcuts(keyboardHandlers())

  return (
    <div className="h-screen overflow-hidden flex flex-col bg-surface text-gray-200">
      <Header isConnected={isConnected} onRefresh={refresh} notificationState={notificationState} selectedWorkflowId={selectedWorkflowId} taskName={status?.task || selectedWorkflowId} onHome={handleHome} />
      <div className="flex-1 flex overflow-hidden">
        <Sidebar
          workflows={workflows}
          selectedWorkflowId={selectedWorkflowId}
          phaseColors={configResult.phaseColors}
          phaseLabels={configResult.phaseLabels}
          onSelect={handleSelect}
          onRefresh={refresh}
          onHome={handleHome}
          collapsed={sidebarCollapsed}
          onToggleCollapse={() => setSidebarCollapsed(v => !v)}
        />
        <main className="flex-1 flex flex-col overflow-hidden">
          <WorkflowView
            selectedWorkflowId={selectedWorkflowId}
            selectedRunId={selectedRunId}
            status={status}
            timeline={timeline}
            config={config}
            now={now}
            showAgents={showAgents}
            workflows={workflows}
            phaseColors={configResult.phaseColors}
            phaseLabels={configResult.phaseLabels}
            phaseIcons={Object.fromEntries((configResult.phases ?? []).map(p => [p.id, p.icon]))}
            onSelect={handleSelect}
            onRefresh={refresh}
          />
        </main>
      </div>

      {/* Keyboard shortcuts help modal */}
      {showShortcutsHelp && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
          onClick={() => setShowShortcutsHelp(false)}
        >
          <div
            className="bg-surface-light border border-gray-700/50 rounded-lg shadow-xl p-6 min-w-64"
            onClick={e => e.stopPropagation()}
          >
            <h3 className="text-sm font-medium text-gray-300 uppercase tracking-wider mb-4">Keyboard Shortcuts</h3>
            <div className="space-y-2 text-sm">
              {[
                ['j', 'Next workflow'],
                ['k', 'Previous workflow'],
                ['h', 'Back to overview'],
                ['r', 'Refresh data'],
                ['a', 'Toggle agents panel'],
                ['s', 'Toggle sidebar'],
                ['?', 'Show/hide this help'],
              ].map(([key, desc]) => (
                <div key={key} className="flex items-center gap-3">
                  <kbd className="px-2 py-0.5 bg-gray-700 rounded text-gray-200 font-mono text-xs border border-gray-600">{key}</kbd>
                  <span className="text-gray-400">{desc}</span>
                </div>
              ))}
            </div>
            <button
              onClick={() => setShowShortcutsHelp(false)}
              className="mt-4 text-xs text-gray-500 hover:text-gray-300"
            >
              Close
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
