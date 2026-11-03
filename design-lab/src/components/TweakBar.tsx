import { useEffect, useRef, useState } from 'react'
import type { Theme } from '../lib/theme'

/*
  Live design-token tuning panel. Reads/writes CSS custom properties on :root so changes
  apply instantly to whichever direction is showing (see src/tokens.css). "Copy CSS"
  exports the current values as :root blocks you can paste back into tokens.css to make a
  tuning session permanent.

  Two things here are less obvious than they look:

  1. Values are held in state as bare numbers ("8"), not authored CSS ("8px"). The unit is
     re-attached on write, on display, and on export. Keeping the unit in state breaks two
     things at once: the label renders "8pxpx", and `<input type="range">` rejects a
     non-numeric value and silently falls back to its own midpoint.

  2. We write to `document.documentElement.style`, which is INLINE and therefore beats both
     `:root` and `:root[data-theme='light']`. So a colour tuned in light mode would leak
     into dark. Fields marked `themed` are tracked per theme, re-applied when the theme
     flips, and exported as two separate blocks.
*/

type FieldType = 'toggle' | 'slider' | 'color' | 'select'

interface Field {
  key: string // CSS custom property name, e.g. --radius
  label: string
  type: FieldType
  min?: number
  max?: number
  step?: number
  unit?: string
  options?: { label: string; value: string }[]
  /*
    True for any token that src/tokens.css also defines in its light block. Those are
    tracked and exported per theme; everything else is shared.
  */
  themed?: boolean
  group: string
}

const FONT_STACKS = [
  { label: 'Inter / system', value: "'Inter', system-ui, sans-serif" },
  { label: 'Grotesk', value: "'Helvetica Neue', Arial, sans-serif" },
  { label: 'Humanist', value: "Optima, Candara, 'Segoe UI', sans-serif" },
  { label: 'Condensed', value: "'Arial Narrow', 'Helvetica Neue', sans-serif" },
  { label: 'Old style serif', value: "'Iowan Old Style', Palatino, Georgia, serif" },
  { label: 'Transitional serif', value: "Charter, 'Bitstream Charter', Georgia, serif" },
  { label: 'Slab serif', value: "Rockwell, 'Roboto Slab', Georgia, serif" },
  { label: 'Mono', value: "'JetBrains Mono', ui-monospace, monospace" },
  { label: 'Typewriter', value: "'Courier New', Courier, monospace" },
]

const EASINGS = [
  { label: 'Snap out', value: 'cubic-bezier(0.2, 0, 0, 1)' },
  { label: 'Ease in-out', value: 'cubic-bezier(0.65, 0, 0.35, 1)' },
  { label: 'Overshoot', value: 'cubic-bezier(0.34, 1.56, 0.64, 1)' },
  { label: 'Linear', value: 'linear' },
]

const FIELDS: Field[] = [
  // Color — every one of these is themed.
  { key: '--color-bg', label: 'Background', type: 'color', themed: true, group: 'Color' },
  { key: '--color-surface', label: 'Surface', type: 'color', themed: true, group: 'Color' },
  { key: '--color-elevated', label: 'Elevated', type: 'color', themed: true, group: 'Color' },
  { key: '--color-fg', label: 'Foreground', type: 'color', themed: true, group: 'Color' },
  { key: '--color-muted', label: 'Muted', type: 'color', themed: true, group: 'Color' },
  { key: '--color-border', label: 'Border', type: 'color', themed: true, group: 'Color' },
  { key: '--color-accent', label: 'Accent', type: 'color', themed: true, group: 'Color' },
  { key: '--color-accent-fg', label: 'On accent', type: 'color', themed: true, group: 'Color' },

  // Type
  { key: '--font-display', label: 'Display face', type: 'select', options: FONT_STACKS, group: 'Type' },
  { key: '--font-body', label: 'Body face', type: 'select', options: FONT_STACKS, group: 'Type' },
  { key: '--font-mono', label: 'Mono face', type: 'select', options: FONT_STACKS, group: 'Type' },
  { key: '--font-scale', label: 'Scale', type: 'slider', min: 0.75, max: 1.6, step: 0.01, group: 'Type' },
  { key: '--weight-display', label: 'Display weight', type: 'slider', min: 100, max: 900, step: 100, group: 'Type' },
  { key: '--weight-body', label: 'Body weight', type: 'slider', min: 100, max: 900, step: 100, group: 'Type' },
  { key: '--tracking-display', label: 'Display tracking', type: 'slider', min: -0.06, max: 0.2, step: 0.005, unit: 'em', group: 'Type' },
  { key: '--tracking-body', label: 'Body tracking', type: 'slider', min: -0.03, max: 0.3, step: 0.005, unit: 'em', group: 'Type' },
  { key: '--leading-display', label: 'Display leading', type: 'slider', min: 0.8, max: 1.6, step: 0.01, group: 'Type' },
  { key: '--leading-body', label: 'Body leading', type: 'slider', min: 1.1, max: 2.2, step: 0.01, group: 'Type' },
  { key: '--measure', label: 'Measure', type: 'slider', min: 30, max: 100, step: 1, unit: 'ch', group: 'Type' },

  // Shape
  { key: '--radius', label: 'Radius', type: 'slider', min: 0, max: 40, step: 1, unit: 'px', group: 'Shape' },
  { key: '--border-width', label: 'Border width', type: 'slider', min: 0, max: 6, step: 1, unit: 'px', group: 'Shape' },
  { key: '--gap', label: 'Gap', type: 'slider', min: 0, max: 64, step: 1, unit: 'px', group: 'Shape' },
  { key: '--pad', label: 'Padding', type: 'slider', min: 0, max: 120, step: 2, unit: 'px', group: 'Shape' },

  // Depth
  { key: '--shadow-strength', label: 'Shadow strength', type: 'slider', min: 0, max: 1, step: 0.01, themed: true, group: 'Depth' },
  { key: '--shadow-y', label: 'Shadow offset', type: 'slider', min: 0, max: 48, step: 1, unit: 'px', group: 'Depth' },

  // Motion
  { key: '--motion-on', label: 'Motion', type: 'toggle', group: 'Motion' },
  { key: '--motion-duration', label: 'Duration x', type: 'slider', min: 0.25, max: 3, step: 0.05, group: 'Motion' },
  { key: '--motion-ease', label: 'Easing', type: 'select', options: EASINGS, group: 'Motion' },
  { key: '--reveal-distance', label: 'Reveal distance', type: 'slider', min: 0, max: 80, step: 1, unit: 'px', group: 'Motion' },
]

const GROUPS = [...new Set(FIELDS.map((f) => f.group))]

function rawVar(key: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(key).trim()
}

/* Sliders hold a bare number; colours and selects hold the authored string. */
function displayValue(f: Field): string {
  const raw = rawVar(f.key)
  return f.type === 'slider' || f.type === 'toggle' ? raw.replace(/[^0-9.-]/g, '') : raw
}

function readAll(): Record<string, string> {
  const out: Record<string, string> = {}
  for (const f of FIELDS) out[f.key] = displayValue(f)
  return out
}

/* `<input type="color">` only accepts #rrggbb, so anything else falls back rather than
   silently resetting the swatch to black on every render. */
function asHex(value: string | undefined): string {
  return value && /^#[0-9a-f]{6}$/i.test(value) ? value : '#000000'
}

function toCss(f: Field, value: string): string {
  return `${value}${f.type === 'slider' ? (f.unit ?? '') : ''}`
}

type ThemeValues = Record<string, string>

/*
  Read what tokens.css declares for BOTH themes, before any tuning has been applied, by
  briefly flipping the theme attribute. Needed so "Copy CSS" can export the theme you are
  not currently looking at. Reading forces a synchronous style recalc but never a paint,
  so nothing flickers.
*/
function readBothThemeDefaults(restoreTo: Theme): Record<Theme, ThemeValues> {
  const root = document.documentElement
  const out: Record<Theme, ThemeValues> = { dark: {}, light: {} }
  for (const theme of ['dark', 'light'] as Theme[]) {
    root.dataset.theme = theme
    out[theme] = readAll()
  }
  root.dataset.theme = restoreTo
  return out
}

export function TweakBar({ theme }: { theme: Theme }) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [copied, setCopied] = useState(false)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({
    Shape: true,
    Depth: true,
    Motion: true,
  })

  /* Only what the user actually changed, per theme. Kept in a ref so the theme-swap effect
     doesn't need to re-run on every keystroke. */
  const editsRef = useRef<Record<Theme, ThemeValues>>({ dark: {}, light: {} })
  const defaultsRef = useRef<Record<Theme, ThemeValues> | null>(null)

  useEffect(() => {
    const root = document.documentElement

    /*
      Write the attribute ourselves before reading anything. React runs child effects before
      parent ones, so this effect fires with the new `theme` prop while useTheme — which
      lives in App, our parent — has not yet put `data-theme` on <html>. Reading computed
      styles at that moment returns the OLD theme's values, which then show up as stale
      numbers in the panel and, worse, get exported into the wrong block. Setting it here is
      idempotent with what useTheme does a moment later.
    */
    root.dataset.theme = theme

    if (!defaultsRef.current) defaultsRef.current = readBothThemeDefaults(theme)

    for (const f of FIELDS) root.style.removeProperty(f.key)
    for (const [key, css] of Object.entries(editsRef.current[theme])) {
      root.style.setProperty(key, css)
    }
    setValues(readAll())
  }, [theme])

  function set(f: Field, value: string) {
    const css = toCss(f, value)
    document.documentElement.style.setProperty(f.key, css)
    /* A themed token belongs to the theme on screen; a shared one applies to both. */
    if (f.themed) {
      editsRef.current[theme][f.key] = css
    } else {
      editsRef.current.dark[f.key] = css
      editsRef.current.light[f.key] = css
    }
    setValues((v) => ({ ...v, [f.key]: value }))
  }

  function reset() {
    editsRef.current = { dark: {}, light: {} }
    for (const f of FIELDS) document.documentElement.style.removeProperty(f.key)
    setValues(readAll())
  }

  async function copyCss() {
    const defaults = defaultsRef.current ?? { dark: {}, light: {} }

    const resolve = (f: Field, t: Theme): string => {
      if (t === theme) return values[f.key] ?? defaults[t][f.key] ?? ''
      const edited = editsRef.current[t][f.key]
      if (edited !== undefined) return f.type === 'slider' ? edited.replace(/[^0-9.-]/g, '') : edited
      return defaults[t][f.key] ?? ''
    }

    const block = (fields: Field[], t: Theme) =>
      fields.map((f) => `  ${f.key}: ${toCss(f, resolve(f, t))};`).join('\n')

    const shared = FIELDS.filter((f) => !f.themed)
    const themed = FIELDS.filter((f) => f.themed)

    const css = [
      ':root {',
      block(shared, 'dark'),
      block(themed, 'dark'),
      '}',
      '',
      ":root[data-theme='light'] {",
      block(themed, 'light'),
      '}',
    ].join('\n')

    await navigator.clipboard.writeText(css)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className="flex h-full w-72 shrink-0 flex-col overflow-hidden border-l border-[var(--chrome-border)] bg-[var(--chrome-bg)] text-xs text-[var(--chrome-fg)]">
      <div className="flex items-center justify-between border-b border-[var(--chrome-border)] px-4 py-2">
        <span className="text-[10px] font-semibold uppercase tracking-widest text-[var(--chrome-muted)]">
          Tokens
        </span>
        <span className="text-[10px] uppercase tracking-wide text-[var(--chrome-muted)]">
          {theme}
        </span>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {GROUPS.map((group) => {
          const isCollapsed = collapsed[group]
          const fields = FIELDS.filter((f) => f.group === group)
          return (
            <div key={group} className="mb-4">
              <button
                onClick={() => setCollapsed((c) => ({ ...c, [group]: !c[group] }))}
                className="mb-2 flex w-full items-center gap-1 text-[10px] font-semibold uppercase tracking-widest text-[var(--chrome-muted)] hover:text-[var(--chrome-fg)]"
              >
                <span>{isCollapsed ? '+' : '−'}</span>
                <span>{group}</span>
                <span className="ml-auto font-normal normal-case tracking-normal">
                  {fields.length}
                </span>
              </button>

              {!isCollapsed && (
                <div className="flex flex-col gap-3">
                  {fields.map((f) => (
                    <div key={f.key}>
                      <div className="mb-1 flex items-center justify-between gap-2">
                        <span className="truncate">{f.label}</span>
                        <span className="shrink-0 text-[var(--chrome-muted)]">
                          {f.type === 'toggle'
                            ? values[f.key] === '1'
                              ? 'ON'
                              : 'OFF'
                            : f.type === 'slider'
                              ? `${values[f.key] ?? ''}${f.unit ?? ''}`
                              : ''}
                        </span>
                      </div>

                      {f.type === 'toggle' && (
                        <button
                          onClick={() => set(f, values[f.key] === '1' ? '0' : '1')}
                          className={`w-full rounded border px-2 py-1 text-center ${
                            values[f.key] === '1'
                              ? 'border-[var(--color-accent)] bg-[var(--chrome-active)]'
                              : 'border-[var(--chrome-border)] bg-transparent text-[var(--chrome-muted)]'
                          }`}
                        >
                          off / on
                        </button>
                      )}

                      {f.type === 'slider' && (
                        <input
                          type="range"
                          min={f.min}
                          max={f.max}
                          step={f.step}
                          value={values[f.key] ?? f.min}
                          onChange={(e) => set(f, e.target.value)}
                          className="w-full accent-[var(--color-accent)]"
                        />
                      )}

                      {f.type === 'color' && (
                        <div className="flex items-center gap-2">
                          <input
                            type="color"
                            value={asHex(values[f.key])}
                            onChange={(e) => set(f, e.target.value)}
                            className="h-7 w-9 shrink-0 cursor-pointer rounded border border-[var(--chrome-border)] bg-transparent"
                          />
                          <input
                            type="text"
                            value={values[f.key] ?? ''}
                            onChange={(e) => set(f, e.target.value)}
                            spellCheck={false}
                            className="w-full rounded border border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)] px-2 py-1 font-mono text-[11px] text-[var(--chrome-fg)] focus:outline-none"
                          />
                        </div>
                      )}

                      {f.type === 'select' && (
                        <select
                          value={values[f.key] ?? ''}
                          onChange={(e) => set(f, e.target.value)}
                          className="w-full rounded border border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)] px-2 py-1 text-[var(--chrome-fg)] focus:outline-none"
                        >
                          {!f.options?.some((o) => o.value === values[f.key]) && (
                            <option value={values[f.key] ?? ''}>(custom)</option>
                          )}
                          {f.options?.map((o) => (
                            <option key={o.value} value={o.value}>
                              {o.label}
                            </option>
                          ))}
                        </select>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className="flex gap-2 border-t border-[var(--chrome-border)] p-4">
        <button
          onClick={copyCss}
          title="Copies both the :root block and the light-theme block"
          className="flex-1 rounded bg-[var(--chrome-hover)] py-2 uppercase tracking-wide hover:bg-[var(--chrome-active)]"
        >
          {copied ? 'Copied' : 'Copy CSS'}
        </button>
        <button
          onClick={reset}
          title="Discards tuning in both themes"
          className="flex-1 rounded bg-[var(--chrome-hover)] py-2 uppercase tracking-wide hover:bg-[var(--chrome-active)]"
        >
          Reset
        </button>
      </div>
    </div>
  )
}
