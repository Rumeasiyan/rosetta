import { Bar, Fat, Pill, Table, body } from '../../parts/parts'
import { terms } from '../../parts/content'
export default function List() {
  return (
    <div style={{ fontFamily: body, minHeight: '100%', background: '#FBF7EE', color: '#24150A', ['--pill-on' as string]: '#FBF7EE' }}>
      <Bar here="Glossary" fg="#24150A" bg="#FBF7EE" line="#3A2414" />
      <div style={{ background: '#3A2414', color: '#FBF7EE', padding: 'clamp(24px,5vw,56px) clamp(16px,4vw,40px)' }}>
        <Fat size="clamp(40px,7vw,84px)">Glossary</Fat>
        <input aria-label="Find a term" placeholder="Find a term in any language" style={{ font: 'inherit', fontSize: 17, width: '100%', maxWidth: 560, minHeight: 48, marginTop: 20, padding: '0 20px', borderRadius: 999, border: '1.5px solid currentColor', background: 'transparent', color: 'inherit' }} />
      </div>
      <div style={{ padding: '20px clamp(16px,4vw,40px) 48px' }}>
        <Table line="#3A2414" soft="#5C4F3F" head={['English', 'සිංහල', 'தமிழ்', 'Type', 'State']} rows={terms.map((t) => [<strong>{t.en}</strong>, <span lang="si">{t.si}</span>, <span lang="ta">{t.ta}</span>, t.type, t.status])} />
        <div style={{ marginTop: 20 }}><Pill color="#24150A" solid>Propose a term</Pill></div>
      </div>
    </div>
  )
}
