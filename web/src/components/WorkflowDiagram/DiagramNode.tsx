import type { PhaseInfo, PhaseColor } from '@/lib/phases'
import { formatDuration } from '@/lib/formatting'

interface DiagramNodeProps {
  phase: PhaseInfo
  colors: PhaseColor
  x: number
  y: number
  w: number
  h: number
  isCurrent: boolean
  isPast: boolean
  isFuture: boolean
  isBlocked: boolean
  totalDurSecs?: number
  iterDurSecs?: number
  hasMultipleIters: boolean
  blockedColors: PhaseColor
}

export default function DiagramNode({
  phase,
  colors,
  x,
  y,
  w,
  h,
  isCurrent,
  isPast,
  isFuture,
  isBlocked,
  totalDurSecs,
  iterDurSecs,
  hasMultipleIters,
  blockedColors,
}: DiagramNodeProps) {
  const opacity = isFuture ? 0.4 : 1
  const ringColor = isBlocked ? blockedColors.ring : colors.ring
  const fillColor = isBlocked
    ? blockedColors.bg
    : isCurrent
    ? colors.bg
    : isPast
    ? colors.bg + 'aa'
    : '#1e293b'
  const strokeColor = isBlocked
    ? blockedColors.ring
    : isCurrent
    ? colors.ring
    : isPast
    ? colors.ring + '88'
    : '#334155'
  const textColor = isCurrent || isPast ? (isBlocked ? blockedColors.text : colors.text) : '#64748b'

  const showDur = !!totalDurSecs && (isCurrent || isPast)
  const showTwoLines = showDur && hasMultipleIters && !!iterDurSecs

  const labelY = showTwoLines ? y + h - 22 : showDur ? y + h - 16 : y + h - 12

  return (
    <g>
      {isCurrent && (
        <rect
          x={x - 5} y={y - 5}
          width={w + 10} height={h + 10}
          rx="15"
          fill="none"
          stroke={ringColor}
          strokeWidth="2.5"
          className="pulse-ring"
        />
      )}

      <rect
        x={x} y={y}
        width={w} height={h}
        rx="10"
        fill={fillColor}
        stroke={strokeColor}
        strokeWidth={isCurrent ? 2 : 1.5}
        opacity={opacity}
        filter={isCurrent ? 'url(#shadow)' : undefined}
        className="node-card"
      />

      {isPast && !isBlocked && (
        <>
          <circle cx={x + w - 10} cy={y + 10} r="7" fill="#16a34a" />
          <path d={`M${x + w - 13},${y + 10} l2,2 4,-4`} stroke="white" strokeWidth="1.5" fill="none" />
        </>
      )}

      {isBlocked && (
        <>
          <circle
            cx={x + w - 10} cy={y + 10} r="8"
            fill={blockedColors.bg}
            stroke={blockedColors.ring}
            strokeWidth="1.5"
          />
          <g transform={`translate(${x + w - 17}, ${y + 3})`}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M10 9v6m4-6v6" />
            </svg>
          </g>
        </>
      )}

      <g transform={`translate(${x + w / 2 - 8}, ${y + 6})`}>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={textColor} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" opacity={opacity}>
          <path d={phase.icon} />
        </svg>
      </g>

      <text
        x={x + w / 2} y={labelY}
        textAnchor="middle"
        fill={textColor}
        fontSize="9"
        fontWeight={isCurrent ? '600' : '500'}
        fontFamily="ui-sans-serif, system-ui, sans-serif"
        opacity={opacity}
      >
        {phase.label}
      </text>

      {showTwoLines && iterDurSecs && totalDurSecs && (
        <>
          <text
            x={x + w / 2} y={y + h - 12}
            textAnchor="middle"
            fill={textColor}
            fontSize="8"
            opacity="0.85"
            fontFamily="ui-monospace, monospace"
          >
            {formatDuration(iterDurSecs * 1000)}
          </text>
          <text
            x={x + w / 2} y={y + h - 2}
            textAnchor="middle"
            fill={textColor}
            fontSize="8"
            opacity="0.55"
            fontFamily="ui-monospace, monospace"
          >
            {'\u03A3\u2009'}{formatDuration(totalDurSecs * 1000)}
          </text>
        </>
      )}

      {showDur && !showTwoLines && totalDurSecs && (
        <text
          x={x + w / 2} y={y + h - 4}
          textAnchor="middle"
          fill={textColor}
          fontSize="8"
          opacity="0.7"
          fontFamily="ui-monospace, monospace"
        >
          {formatDuration(totalDurSecs * 1000)}
        </text>
      )}
    </g>
  )
}
