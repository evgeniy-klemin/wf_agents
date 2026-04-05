import type { NodePosition } from './layout'

interface DiagramEdgeProps {
  label: string
  from: NodePosition
  to: NodePosition
  fromIdx: number
  toIdx: number
  currentIdx: number
  nodeW: number
  nodeH: number
  perRow: number
  depthIdx: number
  baseDepth: number
  depthStep: number
  edgeType: 'forward-adjacent' | 'forward-skip' | 'backward'
}

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

export default function DiagramEdge({
  label,
  from,
  to,
  fromIdx,
  toIdx,
  currentIdx,
  nodeW,
  nodeH,
  perRow,
  depthIdx,
  baseDepth,
  depthStep,
  edgeType,
}: DiagramEdgeProps) {
  const isPast = currentIdx > fromIdx && currentIdx > toIdx
  const isNext = currentIdx === fromIdx

  if (edgeType === 'backward') {
    const depth = baseDepth + depthIdx * depthStep
    const fromRow = Math.floor(fromIdx / perRow)
    const toRow = Math.floor(toIdx / perRow)

    if (fromRow !== toRow) {
      const fx = from.x
      const fy = from.y + nodeH / 2
      const tx = to.x
      const ty = to.y + nodeH / 2
      const leftX = Math.min(from.x, to.x) - depth
      const pathId = `edge-backward-cross-${fromIdx}-${toIdx}`
      const textPathId = `${pathId}-text`
      return (
        <>
          <path
            id={pathId}
            d={`M${fx},${fy} C${leftX},${fy} ${leftX},${ty} ${tx},${ty}`}
            fill="none"
            stroke="#f59e0b"
            strokeWidth="1"
            strokeDasharray="4 3"
            markerEnd="url(#arrow-loop)"
            opacity="0.7"
          />
          {label && (
            <>
              <path
                id={textPathId}
                d={`M${tx},${ty - 10} C${leftX - 18},${ty} ${leftX - 18},${fy} ${fx},${fy - 10}`}
                fill="none"
                stroke="none"
              />
              <text fill="#f59e0b" fontSize="8" opacity="0.8">
                <textPath href={`#${textPathId}`} startOffset="50%" textAnchor="middle">
                  {esc(label)}
                </textPath>
              </text>
            </>
          )}
        </>
      )
    } else {
      const sameRowDepth = 25
      const effectiveDepth = sameRowDepth + depthIdx * 20
      const baseline = Math.max(from.y, to.y) + nodeH
      const fx = from.x + nodeW / 2
      const fy = baseline
      const tx = to.x + nodeW / 2
      const ty = baseline
      const cy1 = fy + effectiveDepth
      const cy2 = ty + effectiveDepth
      const pathId = `edge-backward-same-${fromIdx}-${toIdx}`
      const textPathId = `${pathId}-text`
      return (
        <>
          <path
            id={pathId}
            d={`M${fx},${fy} C${fx},${cy1} ${tx},${cy2} ${tx},${ty}`}
            fill="none"
            stroke="#f59e0b"
            strokeWidth="1"
            strokeDasharray="4 3"
            markerEnd="url(#arrow-loop)"
            opacity="0.7"
          />
          {label && (
            <>
              <path
                id={textPathId}
                d={`M${tx},${ty} C${tx},${cy2 + 10} ${fx},${cy1 + 10} ${fx},${fy}`}
                fill="none"
                stroke="none"
              />
              <text fill="#f59e0b" fontSize="8" opacity="0.8">
                <textPath href={`#${textPathId}`} startOffset="50%" textAnchor="middle">
                  {esc(label)}
                </textPath>
              </text>
            </>
          )}
        </>
      )
    }
  }

  if (edgeType === 'forward-adjacent') {
    const stroke = isPast ? '#6366f1' : isNext ? '#6366f188' : '#334155'
    const strokeW = isPast ? 1.5 : 1
    const marker = isPast || isNext ? 'arrow-active' : 'arrow'
    const activeClass = isNext ? 'flow-active' : undefined

    const fromRow = Math.floor(fromIdx / perRow)
    const toRow = Math.floor(toIdx / perRow)

    if (fromRow === toRow) {
      const fx = from.x + nodeW
      const fy = from.y + nodeH / 2
      const tx = to.x
      const ty = to.y + nodeH / 2
      return (
        <line
          x1={fx} y1={fy} x2={tx} y2={ty}
          stroke={stroke}
          strokeWidth={strokeW}
          markerEnd={`url(#${marker})`}
          className={activeClass}
        />
      )
    } else {
      const fx = from.x + nodeW / 2
      const fy = from.y + nodeH
      const tx = to.x + nodeW / 2
      const ty = to.y
      const midY = (fy + ty) / 2
      const labelX = Math.max(fx, tx) + 10
      return (
        <>
          <path
            d={`M${fx},${fy} C${fx},${midY} ${tx},${midY} ${tx},${ty}`}
            fill="none"
            stroke={stroke}
            strokeWidth={strokeW}
            markerEnd={`url(#${marker})`}
            className={activeClass}
          />
          {label && (
            <text x={labelX} y={midY} textAnchor="start" fill={stroke} fontSize="8" opacity="0.8">
              {esc(label)}
            </text>
          )}
        </>
      )
    }
  }

  // forward-skip: arc ABOVE nodes
  const depth = baseDepth + depthIdx * depthStep
  const topLine = Math.min(from.y, to.y)
  const fx = from.x + nodeW / 2
  const fy = topLine
  const tx = to.x + nodeW / 2
  const ty = topLine
  const cy1 = fy - depth
  const cy2 = ty - depth
  const stroke = isPast ? '#6366f1' : isNext ? '#6366f188' : '#334155'
  const strokeW = isPast ? 1 : 0.75
  const marker = isPast ? 'arrow-active' : 'arrow'
  const pathId = `edge-forward-skip-${fromIdx}-${toIdx}`
  const textPathId = `${pathId}-text`
  return (
    <>
      <path
        id={pathId}
        d={`M${fx},${fy} C${fx},${cy1} ${tx},${cy2} ${tx},${ty}`}
        fill="none"
        stroke={stroke}
        strokeWidth={strokeW}
        markerEnd={`url(#${marker})`}
        opacity="0.7"
      />
      {label && (
        <>
          <path
            id={textPathId}
            d={`M${fx},${fy - 10} C${fx},${cy1 - 18} ${tx},${cy2 - 18} ${tx},${ty - 10}`}
            fill="none"
            stroke="none"
          />
          <text fill={stroke} fontSize="8" opacity="0.8">
            <textPath href={`#${textPathId}`} startOffset="50%" textAnchor="middle">
              {esc(label)}
            </textPath>
          </text>
        </>
      )}
    </>
  )
}
