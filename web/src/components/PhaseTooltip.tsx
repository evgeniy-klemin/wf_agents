interface PhaseTooltipData {
  name: string
  dur: string
  pct: string
  color: string
}

interface Props {
  data: PhaseTooltipData | null
  x: number
  y: number
}

export default function PhaseTooltip({ data, x, y }: Props) {
  if (!data) return null

  let left = x + 12
  if (typeof window !== 'undefined' && left + 160 > window.innerWidth) {
    left = x - 160
  }

  return (
    <div
      className="fixed z-50 pointer-events-none"
      style={{ left, top: y - 70 }}
    >
      <div className="bg-gray-900 border border-gray-700 rounded-lg shadow-xl px-3 py-2 text-xs min-w-[140px]">
        <div className="flex items-center gap-2 mb-1">
          <span
            className="w-2.5 h-2.5 rounded-sm flex-shrink-0"
            style={{ background: data.color }}
          />
          <span className="font-semibold text-gray-100">{data.name}</span>
        </div>
        <div className="text-gray-400 space-y-0.5 pl-4">
          <div>Duration: <span className="text-gray-200 font-medium">{data.dur}</span></div>
          <div>Share: <span className="text-gray-200 font-medium">{data.pct}%</span></div>
        </div>
      </div>
    </div>
  )
}

export type { PhaseTooltipData }
