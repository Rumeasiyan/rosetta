import { Bar, Fat, Pill, Small, body } from '../../parts/parts'
import { recheck } from '../../parts/content'
export default function Memory() {
  return (
    <div style={{ fontFamily: body, minHeight: '100%', background: '#FBF7EE', color: '#24150A', ['--pill-on' as string]: '#FBF7EE' }}>
      <Bar here="Memory" fg="#24150A" bg="#FBF7EE" line="#3A2414" />
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 320px), 1fr))' }}>
        <section style={{ background: '#3A2414', color: '#FBF7EE', padding: 'clamp(24px,4vw,44px)', minHeight: 300 }}>
          <Small>English, as approved</Small>
          <Fat size="clamp(26px,3vw,38px)" lang="en" style={{ marginTop: 22 }}>{recheck.from}</Fat>
        </section>
        <section style={{ background: '#D9C25A', color: '#24150A', padding: 'clamp(24px,4vw,44px)', minHeight: 300 }}>
          <Small>தமிழ், as approved</Small>
          <Fat size="clamp(26px,3vw,38px)" lang="ta" style={{ marginTop: 22 }}>{recheck.to}</Fat>
        </section>
      </div>
      <div style={{ padding: 'clamp(16px,3vw,28px) clamp(16px,4vw,40px)' }}>
        <p style={{ margin: '0 0 4px' }}><strong>{recheck.why}.</strong> {recheck.by}.</p>
        <p style={{ margin: '0 0 18px', color: '#5C4F3F' }}>{recheck.left}.</p>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}><Pill color="#24150A" solid>Still right</Pill><Pill color="#24150A">Correct it</Pill><Pill color="#24150A">Remove</Pill></div>
      </div>
    </div>
  )
}
