export default function DiagramDefs() {
  return (
    <defs>
      <marker id="arrow" markerWidth="5" markerHeight="4" refX="5" refY="2" orient="auto">
        <path d="M0,0 L5,2 L0,4" fill="#475569" />
      </marker>
      <marker id="arrow-active" markerWidth="5" markerHeight="4" refX="5" refY="2" orient="auto">
        <path d="M0,0 L5,2 L0,4" fill="#6366f1" />
      </marker>
      <marker id="arrow-loop" markerWidth="5" markerHeight="4" refX="5" refY="2" orient="auto">
        <path d="M0,0 L5,2 L0,4" fill="#f59e0b" />
      </marker>
      <filter id="glow">
        <feGaussianBlur stdDeviation="3" result="blur" />
        <feMerge>
          <feMergeNode in="blur" />
          <feMergeNode in="SourceGraphic" />
        </feMerge>
      </filter>
      <filter id="shadow">
        <feDropShadow dx="0" dy="2" stdDeviation="3" floodOpacity={0.3} />
      </filter>
    </defs>
  )
}
