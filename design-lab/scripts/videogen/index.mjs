#!/usr/bin/env node
/*
  Video-gen dispatcher. Reads VIDEO_GEN_PROVIDER from the environment (default: "manual" —
  no API call, no cost, prints the prompt for you to paste into Google Flow by hand).
  Set VIDEO_GEN_PROVIDER=replicate|openrouter|atlascloud to turn on a paid API for a
  specific project — off by default is intentional, see .env.example and DECISIONS.md for
  why.

  Usage:
    node scripts/videogen/index.mjs "prompt text" [--duration 8] [--aspect 16:9]
    VIDEO_GEN_PROVIDER=replicate node scripts/videogen/index.mjs "prompt text"
*/
import { generate as manual } from './providers/manual.mjs'
import { generate as replicate } from './providers/replicate.mjs'
import { generate as openrouter } from './providers/openrouter.mjs'
import { generate as atlascloud } from './providers/atlascloud.mjs'

const PROVIDERS = { manual, replicate, openrouter, atlascloud }

const [, , prompt, ...rest] = process.argv
if (!prompt) {
  console.error('Usage: node scripts/videogen/index.mjs "prompt text" [--duration 8] [--aspect 16:9]')
  process.exit(1)
}

function flag(name, fallback) {
  const i = rest.indexOf(`--${name}`)
  return i === -1 ? fallback : rest[i + 1]
}

const providerName = (process.env.VIDEO_GEN_PROVIDER || 'manual').toLowerCase()
const provider = PROVIDERS[providerName]
if (!provider) {
  console.error(`Unknown VIDEO_GEN_PROVIDER "${providerName}". Options: ${Object.keys(PROVIDERS).join(', ')}`)
  process.exit(1)
}

const result = await provider({
  prompt,
  durationSeconds: flag('duration', undefined),
  aspectRatio: flag('aspect', undefined),
})

console.log(JSON.stringify(result, null, 2))
