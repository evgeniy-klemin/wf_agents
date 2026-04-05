import type { PhaseInfo } from '@/lib/phases'

export const MAX_PER_ROW = 8

export interface NodePosition {
  x: number
  y: number
}

export interface DiagramLayout {
  positions: Record<string, NodePosition>
  nodeW: number
  nodeH: number
  gapX: number
  viewBoxX: number
  viewBoxWidth: number
  viewBoxHeight: number
  perRow: number
  numRows: number
}

export function computeLayout(
  phases: PhaseInfo[],
  transitions: [string, string, string][],
  hasMultipleIters: boolean,
): DiagramLayout {
  const numRows = Math.ceil(phases.length / MAX_PER_ROW)
  const perRow = Math.ceil(phases.length / numRows)

  const nodeW = numRows >= 2 ? 80 : 130
  const nodeH = numRows >= 2 ? (hasMultipleIters ? 64 : 56) : (hasMultipleIters ? 72 : 64)
  const gapX = numRows >= 2 ? 30 : 25

  const mainOrder = phases.map(p => p.id)

  const hasCrossRowBackward = transitions.some(([fromId, toId]) => {
    const fromIdx = mainOrder.indexOf(fromId)
    const toIdx = mainOrder.indexOf(toId)
    if (fromIdx < 0 || toIdx < 0 || toIdx >= fromIdx) return false
    return Math.floor(fromIdx / perRow) !== Math.floor(toIdx / perRow)
  })

  const startX = numRows >= 2 && hasCrossRowBackward ? 60 : 30
  const startY = 15
  const rowHeight = nodeH + 60

  const colsInWidestRow = Math.min(phases.length, perRow)
  const totalW = colsInWidestRow * (nodeW + gapX) - gapX + startX * 2
  const totalH = numRows === 1 ? 180 : numRows * rowHeight + 15

  const viewBoxX = numRows >= 2 && hasCrossRowBackward ? -20 : 0

  const positions: Record<string, NodePosition> = {}
  phases.forEach((p, i) => {
    const row = Math.floor(i / perRow)
    const col = i % perRow
    positions[p.id] = { x: startX + col * (nodeW + gapX), y: startY + row * rowHeight }
  })

  return {
    positions,
    nodeW,
    nodeH,
    gapX,
    viewBoxX,
    viewBoxWidth: totalW + (viewBoxX < 0 ? -viewBoxX : 0),
    viewBoxHeight: totalH,
    perRow,
    numRows,
  }
}
