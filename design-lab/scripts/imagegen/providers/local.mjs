/*
  Self-hosted generation server over HTTP. No per-image cost and nothing leaves the machine,
  which makes it the cheapest way to give a direction real imagery instead of a placeholder.

  Configured entirely from the environment — no host path or machine-specific location is
  committed to this repo:

    IMAGE_GEN_BASE_URL   required, e.g. http://127.0.0.1:8189
    IMAGE_GEN_PIPELINE   optional, default "sdxl-text-to-image"
    IMAGE_GEN_MODEL      optional, passed through to the server
    IMAGE_GEN_STEPS      optional, default 25
    IMAGE_GEN_SEED       optional; omit for a random seed (the server returns the one it used)
    IMAGE_GEN_TIMEOUT_MS optional, default 600000 (generation is queued behind one GPU)

  Written against the contract used by story-panel-studio
  (https://github.com/Rumeasiyan/story-panel-studio), which is one such server:

    POST /api/generate            -> { id, status }
    GET  /api/jobs/{id}           -> { status: queued|running|done|error|cancelled, progress, outputs, params, error }
    GET  /api/jobs/{id}/output?index=0 -> the file

  Such a server typically has NO AUTHENTICATION and is meant for a trusted local network.
  Point IMAGE_GEN_BASE_URL at loopback unless you have put auth in front of it.
*/
import { writeFile } from 'node:fs/promises'

const POLL_INTERVAL_MS = 1000

function required(name) {
  const value = process.env[name]
  if (!value) {
    throw new Error(
      `${name} is not set. The local provider needs the base URL of your generation server, ` +
        'e.g. IMAGE_GEN_BASE_URL=http://127.0.0.1:8189 (see .env.example).\n' +
        'If you are an agent: ask the human for this address. Do not scan ports, assume a ' +
        'default host, or search the filesystem for a model directory — on an unfamiliar ' +
        'machine you cannot know it, and guessing wrong wastes their GPU or hits something ' +
        'that is not theirs.',
    )
  }
  return value.replace(/\/+$/, '')
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

export async function generate({ prompt, output, width, height }) {
  const baseUrl = required('IMAGE_GEN_BASE_URL')
  const timeoutMs = Number(process.env.IMAGE_GEN_TIMEOUT_MS ?? 600_000)

  const body = {
    pipeline: process.env.IMAGE_GEN_PIPELINE || 'sdxl-text-to-image',
    prompt,
    width,
    height,
    steps: Number(process.env.IMAGE_GEN_STEPS ?? 25),
  }
  if (process.env.IMAGE_GEN_MODEL) body.model = process.env.IMAGE_GEN_MODEL
  if (process.env.IMAGE_GEN_SEED) body.seed = Number(process.env.IMAGE_GEN_SEED)

  const submit = await fetch(`${baseUrl}/api/generate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!submit.ok) {
    throw new Error(`POST ${baseUrl}/api/generate failed: ${submit.status} ${await submit.text()}`)
  }

  const { id } = await submit.json()
  if (!id) throw new Error('Server accepted the request but returned no job id.')

  const deadline = Date.now() + timeoutMs
  let job
  for (;;) {
    if (Date.now() > deadline) {
      throw new Error(`Job ${id} did not finish within ${timeoutMs}ms. It may still be queued.`)
    }
    await sleep(POLL_INTERVAL_MS)

    const poll = await fetch(`${baseUrl}/api/jobs/${id}`)
    if (!poll.ok) throw new Error(`GET /api/jobs/${id} failed: ${poll.status}`)
    job = await poll.json()

    if (job.status === 'done') break
    if (job.status === 'error') throw new Error(`Job ${id} failed: ${job.error ?? 'no reason given'}`)
    if (job.status === 'cancelled') throw new Error(`Job ${id} was cancelled.`)

    const pct = typeof job.progress === 'number' ? ` ${Math.round(job.progress * 100)}%` : ''
    const queued = job.queue_position ? ` (queue ${job.queue_position})` : ''
    process.stderr.write(`\r[imagegen] ${job.status}${pct}${queued}   `)
  }
  process.stderr.write('\n')

  const file = await fetch(`${baseUrl}/api/jobs/${id}/output?index=0`)
  if (!file.ok) throw new Error(`Fetching the output of job ${id} failed: ${file.status}`)
  await writeFile(output, Buffer.from(await file.arrayBuffer()))

  /* The server echoes the values it actually resolved — including the seed it picked when
     none was given — so record those, not what we asked for. */
  return { path: output, jobId: id, params: job.params ?? null }
}
