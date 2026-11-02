/*
  Atlas Cloud — pay-per-use, cheapest quoted Veo price seen in research ($0.03-0.05/sec),
  but a newer platform with inconsistent pricing across their own marketing pages — verify
  their live pricing page before relying on the number (see
  DECISIONS.md). Requires ATLASCLOUD_API_KEY.
  Docs: https://www.atlascloud.ai/
*/
const DEFAULT_MODEL = 'google/veo-3.1'

export async function generate({ prompt, durationSeconds, aspectRatio, model }) {
  const key = process.env.ATLASCLOUD_API_KEY
  if (!key) {
    throw new Error('ATLASCLOUD_API_KEY not set — see .env.example')
  }

  const res = await fetch('https://api.atlascloud.ai/v1/video/generations', {
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
    throw new Error(`Atlas Cloud request failed: ${res.status} ${await res.text()}`)
  }

  const data = await res.json()
  return { mode: 'atlascloud', response: data }
}
