/*
  OpenRouter — pay-per-use video API (launched 2026), one schema across models. Supports
  Veo 3.1, Seedance, Wan; no Kling as of this research (see
  DECISIONS.md). Requires OPENROUTER_API_KEY.
  Docs: https://openrouter.ai/docs
*/
const DEFAULT_MODEL = 'google/veo-3.1'

export async function generate({ prompt, durationSeconds, aspectRatio, model }) {
  const key = process.env.OPENROUTER_API_KEY
  if (!key) {
    throw new Error('OPENROUTER_API_KEY not set — see .env.example')
  }

  const res = await fetch('https://openrouter.ai/api/v1/videos', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      model: model ?? DEFAULT_MODEL,
      prompt,
      duration: durationSeconds,
      aspect_ratio: aspectRatio,
    }),
  })

  if (!res.ok) {
    throw new Error(`OpenRouter request failed: ${res.status} ${await res.text()}`)
  }

  const data = await res.json()
  return { mode: 'openrouter', response: data }
}
