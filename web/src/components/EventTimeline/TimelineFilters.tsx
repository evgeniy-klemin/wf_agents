interface FilterOption {
  key: string
  label: string
}

const FILTERS: FilterOption[] = [
  { key: 'all', label: 'All' },
  { key: 'transition', label: 'Transitions' },
  { key: 'tool_use', label: 'Tools' },
  { key: 'hook_denial', label: 'Denials' },
  { key: 'agent_spawn', label: 'Agents' },
  { key: 'error', label: 'Errors' },
]

interface Props {
  current: string
  onChange: (filter: string) => void
}

export default function TimelineFilters({ current, onChange }: Props) {
  return (
    <div className="flex gap-1">
      {FILTERS.map(f => (
        <button
          key={f.key}
          onClick={() => onChange(f.key)}
          className={`px-2 py-0.5 text-xs rounded transition-colors ${
            current === f.key
              ? 'bg-surface-lighter text-gray-300'
              : 'text-gray-500 hover:text-gray-300'
          }`}
        >
          {f.label}
        </button>
      ))}
    </div>
  )
}
