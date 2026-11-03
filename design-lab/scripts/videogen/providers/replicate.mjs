/*
  Replicate — pay-per-use, no subscription. Recommended default paid provider (see
  DECISIONS.md): most mature platform, broadest model range
  (Kling, Veo 3.1, Sora 2, Seedance, Wan). Requires REPLICATE_API_TOKEN.
  Docs: https://replicate.com/docs
*/
const DEFAULT_MODEL = 'google/veo-3.1'

export async function generate({ prompt, durationSeconds, aspectRatio, model }) {
  const token = process.env.REPLICATE_API_TOKEN
  if (!token) {
    throw new Error('REPLICATE_API_TOKEN not set — see .env.example')
  }

  const res = await fetch(`https://api.replicate.com/v1/models/${model ?? DEFAULT_MODEL}/predictions`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      Prefer: 'wait',
    },
    body: JSON.stringify({
      input: {
        prompt,
        duration: durationSeconds,
        aspect_ratio: aspectRatio,
      },
    }),
  })

  if (!res.ok) {
    throw new Error(`Replicate request failed: ${res.status} ${await res.text()}`)
  }

  const data = await res.json()
  return { mode: 'replicate', prediction: data }
}
