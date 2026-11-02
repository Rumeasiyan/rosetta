import type { CSSProperties, ReactNode } from 'react'
import '@fontsource/abril-fatface/400.css'
import '@fontsource/noto-serif-sinhala/900.css'
import '@fontsource/noto-serif-tamil/900.css'
import { nav } from './content'

export const fat = "'Abril Fatface', 'Noto Serif Sinhala', 'Noto Serif Tamil', Georgia, serif"
export const body = "system-ui, -apple-system, 'Noto Sans Sinhala', 'Sinhala MN', 'Noto Sans Tamil', 'Tamil Sangam MN', sans-serif"
export const mono = "ui-monospace, 'SF Mono', Menlo, monospace"

export function Fat({ children, size, lang, style }: { children: ReactNode; size: string; lang?: string; style?: CSSProperties }) {
  return <div lang={lang} style={{ fontFamily: fat, fontWeight: 900, fontSize: size, lineHeight: 1.12, overflowWrap: 'anywhere', ...style }}>{children}</div>
}
export function Small({ children, style }: { children: ReactNode; style?: CSSProperties }) {
  return <div style={{ fontSize: 12, letterSpacing: '0.06em', textTransform: 'uppercase', ...style }}>{children}</div>
}
export function Pill({ children, solid, color }: { children: ReactNode; solid?: boolean; color: string }) {
  return (
    <button style={{ font: 'inherit', fontSize: 14, minHeight: 44, padding: '0 20px', borderRadius: 999, border: `1.5px solid ${color}`, background: solid ? color : 'transparent', color: solid ? 'var(--pill-on)' : color, cursor: 'pointer' }}>
      {children}
    </button>
  )
}
export function Bar({ here, fg, bg, line }: { here: string; fg: string; bg: string; line: string }) {
  return (
    <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 22px', padding: '12px clamp(16px,4vw,40px)', background: bg, color: fg, borderBottom: `1.5px solid ${line}`, fontFamily: body, fontSize: 14 }}>
      <strong style={{ fontFamily: "'Abril Fatface', Georgia, serif", fontWeight: 400, fontSize: 21 }}>ROSETTA</strong>
      {nav.map((n) => (
        <span key={n} style={{ padding: '6px 0', borderBottom: n === here ? `2px solid ${fg}` : '2px solid transparent', fontWeight: n === here ? 600 : 400 }}>{n}</span>
      ))}
      <span style={{ marginLeft: 'auto', display: 'flex', gap: 12 }}>
        <span lang="si">සිං</span><span lang="ta">த</span><strong>EN</strong>
      </span>
    </header>
  )
}
const stack = `.r-table{container-type:inline-size}
@container (max-width:600px){
.r-table thead{display:none}
.r-table tr{display:block;padding:12px 0}
.r-table td{display:block;border:0!important;padding:2px 0!important}
}`
export function Table({ head, rows, line, soft }: { head: string[]; rows: ReactNode[][]; line: string; soft: string }) {
  return (
    <div className="r-table">
      <style>{stack}</style>
      <table style={{ borderCollapse: 'collapse', width: '100%', fontFamily: body, fontSize: 15 }}>
        <thead><tr>{head.map((h) => <th key={h} style={{ textAlign: 'left', fontWeight: 500, fontSize: 12, letterSpacing: '0.06em', textTransform: 'uppercase', color: soft, padding: '8px 16px 8px 0', borderBottom: `1.5px solid ${line}` }}>{h}</th>)}</tr></thead>
        <tbody>{rows.map((r, i) => <tr key={i} style={{ borderBottom: `1px solid ${line}` }}>{r.map((c, j) => <td key={j} style={{ padding: '13px 16px 13px 0', verticalAlign: 'top' }}>{c}</td>)}</tr>)}</tbody>
      </table>
    </div>
  )
}
