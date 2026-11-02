import { Bar, Fat, Small, Table, body } from '../../parts/parts'
import { reasons, sources, today } from '../../parts/content'
export default function Status() {
  return (
    <div style={{ fontFamily: body, minHeight: '100%', background: '#FBF7EE', color: '#24150A', ['--pill-on' as string]: '#FBF7EE' }}>
      <Bar here="Status" fg="#24150A" bg="#FBF7EE" line="#3A2414" />
      <div style={{ background: '#D9C25A', color: '#24150A', padding: 'clamp(24px,5vw,52px) clamp(16px,4vw,40px)' }}>
        <Small>BUMBLEBEE answering</Small>
        <Fat size="clamp(36px,6vw,72px)" style={{ marginTop: 10 }}>Working, one source resting</Fat>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))', borderBottom: '1.5px solid #3A2414' }}>
        {today.map((t) => <div key={t.label} style={{ padding: '20px clamp(16px,4vw,40px)' }}><Fat size="36px">{t.value}</Fat><div style={{ fontSize: 14, marginTop: 4 }}>{t.label}, today</div></div>)}
      </div>
      <div style={{ padding: '16px clamp(16px,4vw,40px) 40px', maxWidth: 900 }}>
        <Table line="#3A2414" soft="#5C4F3F" head={['Source', 'State', 'Note']} rows={sources.map((s) => [<strong>{s.name}</strong>, <span style={{ padding: '3px 12px', borderRadius: 999, background: s.state === 'On' ? '#A9C77B' : '#D9C25A', color: '#000' }}>{s.state}</span>, s.note])} />
        <p style={{ fontSize: 14, color: '#5C4F3F' }}>Every source is an outside public website, which receives the text after masking and keeps it under its own terms. Failed today: {reasons.map((r) => r.join(' ')).join(', ')}.</p>
      </div>
    </div>
  )
}
