import { Suspense, useCallback, useState } from 'react'
import { directions } from './lib/directions'
import { useKeyboardNav } from './lib/useKeyboardNav'
import { useTheme } from './lib/theme'
import { DirectionToggle } from './components/DirectionToggle'
import { ScreenNav } from './components/ScreenNav'
import { ThemeToggle } from './components/ThemeToggle'
import { DeviceFrame, DeviceModeSwitch, type DeviceMode } from './components/DeviceFrame'
import { TweakBar } from './components/TweakBar'

export default function App() {
  const [activeId, setActiveId] = useState(directions[0]?.id ?? '')
  const [activeScreenId, setActiveScreenId] = useState(directions[0]?.screens[0]?.id ?? '')
  const [showAll, setShowAll] = useState(false)
  const [device, setDevice] = useState<DeviceMode>('desktop')
  const { theme, toggleTheme } = useTheme()

  const active = directions.find((d) => d.id === activeId)
  const activeScreen = active?.screens.find((s) => s.id === activeScreenId) ?? active?.screens[0]

  const selectDirection = useCallback((id: string) => {
    setActiveId(id)
    setShowAll(false)
    setActiveScreenId(directions.find((d) => d.id === id)?.screens[0]?.id ?? '')
  }, [])

  const toggleAll = useCallback(() => setShowAll((v) => !v), [])

  useKeyboardNav({
    directions,
    activeId,
    activeScreenId,
    showAll,
    onSelectDirection: selectDirection,
    onSelectScreen: setActiveScreenId,
    onToggleAll: toggleAll,
    onToggleTheme: toggleTheme,
  })

  return (
    <div className="flex h-screen flex-col bg-[var(--chrome-bg)]">
      <DirectionToggle
        directions={directions}
        activeId={activeId}
        showAll={showAll}
        onSelect={selectDirection}
        onToggleAll={toggleAll}
      />

      <div className="flex items-center justify-between gap-3 border-b border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)] px-3 py-2">
        <span className="truncate text-xs uppercase tracking-wide text-[var(--chrome-muted)]">
          {showAll ? 'All directions' : (active?.label ?? 'No direction')}
          {!showAll && active?.sample && (
            <span className="ml-2 text-[var(--color-accent)]">
              sample — delete before building
            </span>
          )}
        </span>
        <div className="flex shrink-0 items-center gap-2">
          <ThemeToggle theme={theme} onToggle={toggleTheme} />
          <DeviceModeSwitch mode={device} onChange={setDevice} />
        </div>
      </div>

      <div className="flex min-h-0 flex-1">
        {!showAll && active && (
          <ScreenNav
            screens={active.screens}
            activeId={activeScreen?.id ?? ''}
            onSelect={setActiveScreenId}
          />
        )}

        <div className="min-w-0 flex-1">
          <Suspense
            fallback={<div className="p-12 text-[var(--chrome-muted)]">Loading...</div>}
          >
            {showAll ? (
              <div className="grid h-full grid-cols-2 gap-4 overflow-auto bg-[var(--chrome-bg-alt)] p-4 lg:grid-cols-3">
                {directions.map((d) => {
                  const Comp = d.screens[0]?.component
                  return (
                    <div
                      key={d.id}
                      onClick={() => selectDirection(d.id)}
                      className="cursor-pointer overflow-hidden rounded border border-[var(--chrome-border)]"
                      style={{ background: 'var(--color-bg)' }}
                    >
                      <div className="flex items-center gap-1.5 border-b border-[var(--chrome-border)] bg-[var(--chrome-bg)] px-2 py-1 text-[10px] uppercase text-[var(--chrome-muted)]">
                        {d.label}
                        {d.sample && (
                          <span className="text-[var(--color-accent)]">sample</span>
                        )}
                      </div>
                      <div className="h-64 overflow-hidden" style={{ fontSize: 'calc(16px * var(--font-scale))' }}>
                        {Comp && <Comp />}
                      </div>
                    </div>
                  )
                })}
              </div>
            ) : (
              activeScreen && (
                <DeviceFrame mode={device}>
                  <activeScreen.component />
                </DeviceFrame>
              )
            )}
          </Suspense>
        </div>

        <TweakBar theme={theme} />
      </div>
    </div>
  )
}
