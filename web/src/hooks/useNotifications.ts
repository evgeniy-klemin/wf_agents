import { useRef, useEffect, useState } from 'react'
import type { WorkflowStatus } from '@/types/api'
import { STUCK_THRESHOLD_MS } from '@/lib/stuck'

function playBeep() {
  try {
    const ctx = new AudioContext()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.frequency.value = 440
    osc.type = 'sine'
    gain.gain.setValueAtTime(0.3, ctx.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.2)
    osc.start(ctx.currentTime)
    osc.stop(ctx.currentTime + 0.2)
  } catch {
    // AudioContext not available
  }
}

function sendNotification(title: string, body: string, tag: string, soundEnabled: boolean) {
  if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return
  new Notification(title, { body, tag })
  if (soundEnabled) playBeep()
}

export function useNotifications(
  workflowId: string | null,
  status: WorkflowStatus | null,
  isRunning: boolean,
  now: number
): {
  permissionState: string
  requestPermission: () => void
  soundEnabled: boolean
  toggleSound: () => void
} {
  const [permissionState, setPermissionState] = useState<string>(
    typeof Notification !== 'undefined' ? Notification.permission : 'default'
  )
  const [soundEnabled, setSoundEnabled] = useState<boolean>(() => {
    try {
      return localStorage.getItem('wf-sound-enabled') === 'true'
    } catch {
      return false
    }
  })

  const prevPhase = useRef<string | null>(null)
  const prevStuck = useRef<boolean>(false)

  const isStuck =
    isRunning &&
    status !== null &&
    status.phase !== 'BLOCKED' &&
    status.phase !== 'COMPLETE' &&
    !!status.last_updated_at &&
    now - new Date(status.last_updated_at).getTime() > STUCK_THRESHOLD_MS

  useEffect(() => {
    if (!workflowId || !status) {
      prevPhase.current = null
      prevStuck.current = false
      return
    }

    const phase = status.phase
    const previousPhase = prevPhase.current
    const wasStuck = prevStuck.current

    // BLOCKED transition
    if (phase === 'BLOCKED' && previousPhase !== 'BLOCKED') {
      sendNotification(
        '⏸ Workflow blocked',
        status.pre_blocked_phase ? `Was in: ${status.pre_blocked_phase}` : 'Workflow is blocked',
        `wf-blocked-${workflowId}`,
        soundEnabled
      )
    }

    // COMPLETE transition
    if (phase === 'COMPLETE' && previousPhase !== 'COMPLETE') {
      sendNotification(
        '✅ Workflow complete',
        status.task ? `Task: ${status.task}` : 'Workflow has completed',
        `wf-complete-${workflowId}`,
        soundEnabled
      )
    }

    // Stuck transition
    if (isStuck && !wasStuck) {
      const idleMs = now - new Date(status.last_updated_at).getTime()
      const idleMin = Math.floor(idleMs / 60000)
      sendNotification(
        '⚠️ Workflow stuck',
        `Idle for ${idleMin} minute${idleMin !== 1 ? 's' : ''}`,
        `wf-stuck-${workflowId}`,
        soundEnabled
      )
    }

    prevPhase.current = phase
    prevStuck.current = isStuck
  }, [workflowId, status, isStuck, now, soundEnabled])

  function requestPermission() {
    if (typeof Notification === 'undefined') return
    Notification.requestPermission().then((result) => {
      setPermissionState(result)
    })
  }

  function toggleSound() {
    setSoundEnabled((prev) => {
      const next = !prev
      try {
        localStorage.setItem('wf-sound-enabled', String(next))
      } catch {
        // localStorage not available
      }
      return next
    })
  }

  return { permissionState, requestPermission, soundEnabled, toggleSound }
}
