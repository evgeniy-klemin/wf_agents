export function formatDuration(ms: number): string {
  if (ms < 0) ms = 0
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rs = s % 60
  if (m < 60) return `${m}m ${rs}s`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return `${h}h ${rm}m`
}

export function timeAgo(iso: string): string {
  const d = new Date(iso)
  const diff = (Date.now() - d.getTime()) / 1000
  if (diff < 60) return 'just now'
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago'
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago'
  return Math.floor(diff / 86400) + 'd ago'
}

export function truncate(s: string, max: number): string {
  if (!s) return ''
  s = s.replace(/\n/g, ' ').trim()
  return s.length > max ? s.substring(0, max) + '...' : s
}

export function shortPath(p: string): string {
  if (!p) return ''
  const parts = p.split('/')
  if (parts.length <= 3) return p
  return '.../' + parts.slice(-3).join('/')
}

export function mrLabel(url: string): string {
  if (!url) return ''
  const m = url.match(/\/merge_requests\/(\d+)/)
  return m ? `!${m[1]}` : 'MR'
}

// statusColor returns the hex color for a workflow status dot/favicon.
// Matches the favicon color scheme in App.tsx.
export function statusColor(phase: string | null, stuck: boolean): string {
  if (phase === 'BLOCKED') return '#dc2626'
  if (stuck) return '#f97316'
  if (phase === 'COMPLETE' || phase === null || phase === '') return '#6b7280'
  return '#22c55e'
}

export function categoryLabel(category: string): string {
  if (category === '_file_changed') return 'files'
  if (category === '_sent_message') return 'msg'
  return category
}
