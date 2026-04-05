interface NotificationState {
  permissionState: string
  requestPermission: () => void
  soundEnabled: boolean
  toggleSound: () => void
}

interface HeaderProps {
  isConnected: boolean
  onRefresh: () => void
  notificationState: NotificationState
  selectedWorkflowId?: string | null
  taskName?: string | null
  onHome?: () => void
}

export default function Header({ isConnected, onRefresh, notificationState, selectedWorkflowId, taskName, onHome }: HeaderProps) {
  const { permissionState, requestPermission, soundEnabled, toggleSound } = notificationState

  return (
    <header className="bg-surface-light border-b border-gray-700/50 px-6 py-3 flex items-center justify-between flex-shrink-0">
      <div className="flex items-center gap-3">
        <div className="w-8 h-8 rounded-lg bg-accent flex items-center justify-center flex-shrink-0">
          <svg className="w-5 h-5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
        </div>
        {selectedWorkflowId && onHome ? (
          <div className="flex items-center gap-2 min-w-0">
            <button
              onClick={onHome}
              className="text-sm font-medium text-accent-light hover:text-white transition-colors whitespace-nowrap"
            >
              Workflow Dashboard
            </button>
            <span className="text-gray-500">/</span>
            <span className="text-sm font-semibold text-white">
              {taskName || selectedWorkflowId}
            </span>
          </div>
        ) : (
          <h1 className="text-lg font-semibold text-white">Workflow Dashboard</h1>
        )}
      </div>
      <div className="flex items-center gap-3">
        {isConnected ? (
          <span className="flex items-center gap-1.5 text-xs text-green-400">
            <span className="w-2 h-2 rounded-full bg-green-400" />
            Connected
          </span>
        ) : (
          <span className="flex items-center gap-1.5 text-xs text-red-400">
            <span className="w-2 h-2 rounded-full bg-red-400" />
            Disconnected
          </span>
        )}

        {/* Bell icon for notifications */}
        {permissionState === 'denied' ? (
          <button
            title="Notifications blocked in browser settings"
            className="p-1.5 rounded-lg text-gray-500 cursor-not-allowed"
            disabled
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 3l18 18" />
            </svg>
          </button>
        ) : permissionState === 'granted' ? (
          <button
            title="Notifications enabled"
            className="p-1.5 rounded-lg text-yellow-400"
            disabled
          >
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
              <path d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
            </svg>
          </button>
        ) : (
          <button
            onClick={requestPermission}
            title="Enable notifications"
            className="p-1.5 rounded-lg text-gray-400 hover:text-gray-200 hover:bg-surface-lighter transition-colors"
          >
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
            </svg>
          </button>
        )}

        {/* Sound toggle */}
        <button
          onClick={toggleSound}
          title={soundEnabled ? 'Sound on' : 'Sound off'}
          className={`p-1.5 rounded-lg transition-colors ${soundEnabled ? 'text-blue-400 hover:bg-surface-lighter' : 'text-gray-500 hover:text-gray-300 hover:bg-surface-lighter'}`}
        >
          {soundEnabled ? (
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5L6 9H2v6h4l5 4V5z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.536 8.464a5 5 0 010 7.072" />
            </svg>
          ) : (
            <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5L6 9H2v6h4l5 4V5z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2" />
            </svg>
          )}
        </button>

        <button
          onClick={onRefresh}
          className="px-3 py-1.5 text-sm bg-surface-lighter hover:bg-gray-600 rounded-lg transition-colors flex items-center gap-1.5"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </button>
      </div>
    </header>
  )
}
