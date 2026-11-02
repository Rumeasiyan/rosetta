import { Bar, Fat, Pill, Small, body } from '../parts/parts'
import { langs, term } from '../parts/content'
const bands = [
  { l: langs[0], text: term.si, bg: '#A79F90', fg: '#24150A' },
  { l: langs[1], text: term.ta, bg: '#D9C25A', fg: '#24150A' },
  { l: langs[2], text: term.en, bg: '#3A2414', fg: '#FBF7EE' },
]
export default function Page() {
  return (
    <div style={{ fontFamily: body, minHeight: '100%', background: '#FBF7EE', color: '#24150A', ['--pill-on' as string]: '#FBF7EE' }}>
      <Bar here="Glossary" fg="#24150A" bg="#FBF7EE" line="#3A2414" />
      {bands.map((b) => (
        <section key={b.l.code} style={{ background: b.bg, color: b.fg, display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: '4px 28px', padding: 'clamp(22px,3.4vw,40px) clamp(16px,4vw,40px)' }}>
          <Small style={{ width: 90 }}>{b.l.name}</Small>
          <Fat lang={b.l.code} size="clamp(34px,6vw,72px)" style={{ flex: '1 1 300px' }}>{b.text}</Fat>
          <span style={{ fontSize: 14 }}>{b.l.keeper}: {b.l.when}</span>
        </section>
      ))}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center', padding: 'clamp(16px,3vw,28px) clamp(16px,4vw,40px)' }}>
        <div style={{ marginRight: 'auto' }}><strong>{term.type}.</strong> {term.note} {term.version}.</div>
        <Pill color="#3A2414">History</Pill><Pill color="#3A2414" solid>Propose a correction</Pill>
      </div>
    </div>
  )
}
