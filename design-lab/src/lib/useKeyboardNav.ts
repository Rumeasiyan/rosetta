import { useEffect } from 'react'
import type { Direction } from './directions'

/*
  All of the harness's keyboard shortcuts live here, in one handler, because directions and
  screens have to agree about the arrow keys:

    1-9          select a direction
    Left/Right   previous / next direction (wraps)
    Up/Down      previous / next screen within the active direction (wraps)
    G            toggle the all-directions grid
    D            toggle light/dark

  Arrow keys are preventDefault-ed so they don't scroll the page. The INPUT/TEXTAREA guard
  matters more than it looks: TweakBar's sliders are <input type="range">, which is driven
  by the arrow keys — without the guard, nudging a slider would also change direction.
*/

function cycle<T>(items: T[], currentIndex: number, delta: number): T | undefined {
  if (items.length === 0) return undefined
  const next = (currentIndex + delta + items.length) % items.length
  return items[next]
}

export function useKeyboardNav({
  directions,
  activeId,
  activeScreenId,
  showAll,
  onSelectDirection,
  onSelectScreen,
  onToggleAll,
  onToggleTheme,
}: {
  directions: Direction[]
  activeId: string
  activeScreenId: string
  showAll: boolean
  onSelectDirection: (id: string) => void
  onSelectScreen: (id: string) => void
  onToggleAll: () => void
  onToggleTheme: () => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return

      const target = e.target as HTMLElement | null
      if (target) {
        const tag = target.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || target.isContentEditable) return
      }

      switch (e.key) {
        case 'ArrowLeft':
        case 'ArrowRight': {
          e.preventDefault()
          const i = directions.findIndex((d) => d.id === activeId)
          const next = cycle(directions, i, e.key === 'ArrowRight' ? 1 : -1)
          if (next) onSelectDirection(next.id)
          return
        }
        case 'ArrowUp':
        case 'ArrowDown': {
          if (showAll) return
          const screens = directions.find((d) => d.id === activeId)?.screens ?? []
          if (screens.length < 2) return
          e.preventDefault()
          const i = screens.findIndex((s) => s.id === activeScreenId)
          const next = cycle(screens, i, e.key === 'ArrowDown' ? 1 : -1)
          if (next) onSelectScreen(next.id)
          return
        }
      }

      const key = e.key.toLowerCase()
      if (key === 'g') {
        onToggleAll()
        return
      }
      if (key === 'd') {
        onToggleTheme()
        return
      }

      const match = directions.find((d) => d.key === e.key)
      if (match) onSelectDirection(match.id)
    }

    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [
    directions,
    activeId,
    activeScreenId,
    showAll,
    onSelectDirection,
    onSelectScreen,
    onToggleAll,
    onToggleTheme,
  ])
}
