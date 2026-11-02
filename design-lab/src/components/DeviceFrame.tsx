import { type ReactNode } from 'react'

export type DeviceMode = 'mobile' | 'tablet' | 'desktop'

const WIDTHS: Record<DeviceMode, string> = {
  mobile: '390px',
  tablet: '834px',
  desktop: '100%',
}

/*
  The preview's base font-size is derived from --font-scale, which is what makes the
  TweakBar's Font Scale slider do anything. Directions must therefore size type in `em`,
  not rem/px — a rem resolves against the document root and would ignore this entirely.
  The scale deliberately stops at this boundary so the harness chrome never resizes.
*/
export function DeviceFrame({ mode, children }: { mode: DeviceMode; children: ReactNode }) {
  return (
    <div className="flex h-full w-full items-start justify-center overflow-auto bg-[var(--chrome-bg-alt)] p-6">
      <div
        className="h-[calc(100vh-160px)] overflow-auto shadow-2xl transition-[width] duration-300"
        style={{
          width: WIDTHS[mode],
          maxWidth: '100%',
          background: 'var(--color-bg)',
          fontSize: 'calc(16px * var(--font-scale))',
        }}
      >
        {children}
      </div>
    </div>
  )
}

export function DeviceModeSwitch({
  mode,
  onChange,
}: {
  mode: DeviceMode
  onChange: (m: DeviceMode) => void
}) {
  const modes: DeviceMode[] = ['mobile', 'tablet', 'desktop']
  return (
    <div className="flex gap-1 rounded-md bg-[var(--chrome-bg-alt)] p-1 text-xs">
      {modes.map((m) => (
        <button
          key={m}
          onClick={() => onChange(m)}
          className={`rounded px-2 py-1 uppercase tracking-wide ${
            mode === m
              ? 'bg-[var(--chrome-active)] text-[var(--chrome-fg)]'
              : 'text-[var(--chrome-muted)] hover:text-[var(--chrome-fg)]'
          }`}
        >
          {m}
        </button>
      ))}
    </div>
  )
}
