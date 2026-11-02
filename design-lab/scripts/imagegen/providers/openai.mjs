/*
  OpenAI Images API. Costs money per image — opt in per project by setting
  IMAGE_GEN_PROVIDER=openai.

    OPENAI_API_KEY   required
    IMAGE_GEN_MODEL  optional, default "gpt-image-1"

  The API returns base64 rather than a URL for this model, so the bytes are written straight
  to disk. Sizes must be one the model accepts; the nearest supported square/landscape/
  portrait is chosen from the requested width/height.
*/
import { writeFile } from 'node:fs/promises'

function pickSize(width, height) {
  const ratio = width / height
  if (ratio > 1.2) return '1536x1024'
  if (ratio < 0.83) return '1024x1536'
  return '1024x1024'
}

export async function generate({ prompt, output, width, height }) {
  const apiKey = process.env.OPENAI_API_KEY
  if (!apiKey) {
    throw new Error('OPENAI_API_KEY is not set (see .env.example).')
  }

  /* The only provider here that bills per call. Say so, every time, so a run that was
     started on the agent's initiative is at least visible in the log. */
  console.error('[imagegen] openai is a PAID provider — this call bills your API account.')

  const response = await fetch('https://api.openai.com/v1/images/generations', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify({
      model: process.env.IMAGE_GEN_MODEL || 'gpt-image-1',
      prompt,
      size: pickSize(width, height),
      n: 1,
    }),
  })

  if (!response.ok) {
    throw new Error(`OpenAI images request failed: ${response.status} ${await response.text()}`)
  }

  const payload = await response.json()
  const b64 = payload.data?.[0]?.b64_json
  if (!b64) {
    throw new Error(`OpenAI returned no image data: ${JSON.stringify(payload).slice(0, 400)}`)
  }

  await writeFile(output, Buffer.from(b64, 'base64'))
  return { path: output }
}
