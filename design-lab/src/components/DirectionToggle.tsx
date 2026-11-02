import { useState } from 'react'
import type { Direction } from '../lib/directions'

/*
  Top tab bar: one tab per registered direction, plus the grid toggle. Keyboard shortcuts
  are NOT handled here — they live in src/lib/useKeyboardNav.ts, because directions
  (Left/Right, 1-9) and screens (Up/Down) have to share one handler.
*/
export function DirectionToggle({
  directions,
  activeId,
  showAll,
  onSelect,
  onToggleAll,
}: {
  directions: Direction[]
  activeId: string
  showAll: boolean
  onSelect: (id: string) => void
  onToggleAll: () => void
}) {
  const [query, setQuery] = useState('')

  const q = query.trim().toLowerCase()
  const filtered = q
    ? directions.filter((d) => `${d.label} ${d.sub ?? ''}`.toLowerCase().includes(q))
    : directions

  return (
    <div className="flex items-stretch gap-0.5 overflow-x-auto border-b border-[var(--chrome-border)] bg-[var(--chrome-bg)] px-2 text-xs">
      <span className="shrink-0 self-center px-2 py-2 font-semibold uppercase tracking-wide text-[var(--chrome-muted)]">
        {directions.length} Directions
      </span>

      {directions.length > 6 && (
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter..."
          className="mx-1 w-28 shrink-0 self-center rounded border border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)] px-2 py-1 text-[var(--chrome-fg)] placeholder:text-[var(--chrome-muted)] focus:outline-none"
        />
      )}

      {filtered.length === 0 ? (
        <span className="shrink-0 self-center px-2 py-2 text-[var(--chrome-muted)]">No matches</span>
      ) : (
        filtered.map((d) => {
          const isActive = !showAll && activeId === d.id
          return (
            <button
              key={d.id}
              onClick={() => onSelect(d.id)}
              className={`flex shrink-0 flex-col items-start border-r border-[var(--chrome-border)] px-3 py-2 text-left ${
                isActive
                  ? 'bg-[var(--chrome-active)] text-[var(--chrome-fg)]'
                  : 'text-[var(--chrome-muted)] hover:bg-[var(--chrome-hover)] hover:text-[var(--chrome-fg)]'
              }`}
            >
              <span className="flex items-center gap-1.5 font-semibold uppercase">
                {d.label}
                {d.sample && (
                  <span
                    title="Template example — delete with: node scripts/remove-samples.mjs"
                    className="rounded-sm border border-[var(--color-accent)] px-1 text-[9px] font-semibold uppercase tracking-wide text-[var(--color-accent)]"
                  >
                    Sample
                  </span>
                )}
              </span>
              {d.sub && (
                <span className="text-[10px] uppercase text-[var(--chrome-muted)]">{d.sub}</span>
              )}
            </button>
          )
        })
      )}

      <button
        onClick={onToggleAll}
        className={`ml-auto shrink-0 px-3 py-2 font-semibold uppercase tracking-wide ${
          showAll
            ? 'bg-[var(--chrome-active)] text-[var(--chrome-fg)]'
            : 'text-[var(--chrome-muted)] hover:bg-[var(--chrome-hover)] hover:text-[var(--chrome-fg)]'
        }`}
      >
        All {directions.length}
      </button>

      <span className="shrink-0 self-center px-2 py-2 text-[10px] uppercase tracking-wide text-[var(--chrome-muted)]">
        1-9 · ← → dir · ↑ ↓ screen · G grid · D theme
      </span>
    </div>
  )
}
