#!/usr/bin/env node
/*
  Image-gen dispatcher. Reads IMAGE_GEN_PROVIDER from the environment (default: "codex" -
  wraps `codex exec`'s built-in image tool, billed against your existing ChatGPT Plus
  session, no separate API key).

  If you are an agent: ASK THE HUMAN BEFORE RUNNING THIS. Every provider costs them
  something - `codex` spends their ChatGPT Plus session, `openai` bills per image, `local`
  occupies their GPU. Propose the prompts, paths and provider, then wait. See AGENTS.md.

  Images are not optional decoration in this harness. A direction built only from type and
  CSS boxes reads as machine-generated, which is exactly what a design review is meant to
  catch - so generate real assets rather than leaving a gap or a grey placeholder. See the
  "Assets" section of README.md.

  Usage:
    node scripts/imagegen/index.mjs "prompt text" [output/path.png] [--width 1024] [--height 1024] [--approved]
    IMAGE_GEN_PROVIDER=local node scripts/imagegen/index.mjs "prompt" src/directions/v1/hero.png

  Providers:
    codex   (default) `codex exec` image tool - no API key, uses your codex login
    local   a self-hosted generation server over HTTP - set IMAGE_GEN_BASE_URL
    openai  the OpenAI Images API - set OPENAI_API_KEY

  --approved: pass it only when the human has said yes to this exact batch (prompts, paths,
  provider) and that yes is recorded in the conversation. The codex provider then tells
  codex so in the prompt; without it, codex reads the project's AGENTS.md rule "never
  generate without asking", asks for confirmation, and saves nothing. Never pass it to get
  past that question on your own: the rule exists for you, not for codex.
*/
import { mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

import { generate as codex } from './providers/codex.mjs'
import { generate as local } from './providers/local.mjs'
import { generate as openai } from './providers/openai.mjs'

const PROVIDERS = { codex, local, openai }

const [, , prompt, ...rest] = process.argv
if (!prompt || prompt.startsWith('--')) {
  console.error(
    'Usage: node scripts/imagegen/index.mjs "prompt text" [output/path.png] [--width 1024] [--height 1024]',
  )
  process.exit(1)
}

function flag(name, fallback) {
  const i = rest.indexOf(`--${name}`)
  return i === -1 ? fallback : rest[i + 1]
}

/* Flags that take no value. */
const BOOLEAN = new Set(['--approved'])
const approved = rest.includes('--approved')

/* Positional args are the ones that are neither a --flag nor a --flag's value. */
const positional = []
for (let i = 0; i < rest.length; i++) {
  if (BOOLEAN.has(rest[i])) continue
  if (rest[i].startsWith('--')) i++
  else positional.push(rest[i])
}

const output = resolve(positional[0] ?? `output/imagegen/${Date.now()}.png`)
mkdirSync(dirname(output), { recursive: true })

const providerName = (process.env.IMAGE_GEN_PROVIDER || 'codex').toLowerCase()
const provider = PROVIDERS[providerName]
if (!provider) {
  console.error(
    `Unknown IMAGE_GEN_PROVIDER "${providerName}". Options: ${Object.keys(PROVIDERS).join(', ')}`,
  )
  process.exit(1)
}

console.error(`[imagegen] provider: ${providerName}`)
console.error(`[imagegen] prompt:   ${prompt}`)
console.error(`[imagegen] output:   ${output}`)
console.error(`[imagegen] approved: ${approved ? 'yes (human approved this batch)' : 'no'}`)

let result
try {
  result = await provider({
    prompt,
    output,
    width: Number(flag('width', 1024)),
    height: Number(flag('height', 1024)),
    approved,
  })
} catch (error) {
  /* A stack trace here is noise - the useful part is always the message (a missing key, an
     unreachable server, a rejected prompt). */
  console.error(`[imagegen] ${providerName} provider failed: ${error.message}`)
  process.exit(1)
}

console.log(JSON.stringify({ provider: providerName, ...result }, null, 2))
