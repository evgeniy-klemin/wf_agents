import { useEffect } from 'react'

export function useKeyboardShortcuts(handlers: Record<string, () => void>): void {
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const tag = (document.activeElement as HTMLElement)?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'textarea') return

      const handler = handlers[e.key]
      if (handler) {
        e.preventDefault()
        handler()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [handlers])
}
