import type { Screen } from '../lib/directions'

/*
  Vertical rail on the left of the preview, listing the active direction's screens.
  Renders nothing for a single-screen direction — a nav with one item is just a label.
  Up/Down cycle the selection; the handler lives in src/lib/useKeyboardNav.ts so screens
  and directions can't disagree about the arrow keys.
*/
export function ScreenNav({
  screens,
  activeId,
  onSelect,
}: {
  screens: Screen[]
  activeId: string
  onSelect: (id: string) => void
}) {
  if (screens.length < 2) return null

  return (
    <nav className="flex w-44 shrink-0 flex-col border-r border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)]">
      <span className="px-3 py-2 text-[10px] font-semibold uppercase tracking-widest text-[var(--chrome-muted)]">
        Screens
      </span>

      <div className="flex-1 overflow-y-auto pb-2">
        {screens.map((s, i) => {
          const isActive = activeId === s.id
          return (
            <button
              key={s.id}
              onClick={() => onSelect(s.id)}
              aria-current={isActive ? 'page' : undefined}
              className={`flex w-full items-center gap-2 border-l-2 px-3 py-2 text-left text-xs uppercase tracking-wide ${
                isActive
                  ? 'border-[var(--color-accent)] bg-[var(--chrome-active)] text-[var(--chrome-fg)]'
                  : 'border-transparent text-[var(--chrome-muted)] hover:bg-[var(--chrome-hover)] hover:text-[var(--chrome-fg)]'
              }`}
            >
              <span className="w-4 shrink-0 text-[10px] tabular-nums text-[var(--chrome-muted)]">
                {i + 1}
              </span>
              <span className="truncate">{s.label}</span>
            </button>
          )
        })}
      </div>

      <span className="border-t border-[var(--chrome-border)] px-3 py-2 text-[10px] uppercase tracking-wide text-[var(--chrome-muted)]">
        ↑ ↓ to move
      </span>
    </nav>
  )
}
