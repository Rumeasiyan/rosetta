#!/usr/bin/env node
/*
  Scaffolds a new screen inside an existing direction: creates
  src/directions/<directionId>/screens/<screenId>.tsx and appends it to that
  direction's `screens` array in src/lib/directions.ts. Mirrors new-direction.mjs —
  see USAGE.md for the multi-screen workflow.

  Usage:
    node scripts/new-screen.mjs <directionId> <screenId> --label "Detail"
*/
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const DIRECTIONS_TS = resolve(ROOT, 'src/lib/directions.ts')

const [, , directionId, screenId, ...rest] = process.argv

function usage() {
  console.error('Usage: node scripts/new-screen.mjs <directionId> <screenId> --label "Detail"')
  process.exit(1)
}

if (!directionId || !screenId || directionId.startsWith('--') || screenId.startsWith('--')) usage()
if (!/^[a-z0-9-]+$/.test(screenId)) {
  console.error(`Invalid screen id "${screenId}" — use lowercase letters, digits, hyphens (e.g. detail, settings-2).`)
  process.exit(1)
}

function flag(name, fallback) {
  const i = rest.indexOf(`--${name}`)
  return i === -1 ? fallback : rest[i + 1]
}

const label = flag('label', screenId.charAt(0).toUpperCase() + screenId.slice(1))

const dirPath = resolve(ROOT, `src/directions/${directionId}`)
const screensDir = resolve(dirPath, 'screens')
const screenPath = resolve(screensDir, `${screenId}.tsx`)

if (!existsSync(dirPath)) {
  console.error(`${dirPath} doesn't exist — create the direction first with scripts/new-direction.mjs.`)
  process.exit(1)
}
if (existsSync(screenPath)) {
  console.error(`${screenPath} already exists — pick a different screen id or edit it directly.`)
  process.exit(1)
}

let registry = readFileSync(DIRECTIONS_TS, 'utf8')

const idMarker = `id: '${directionId}',`
const directionIdx = registry.indexOf(idMarker)
if (directionIdx === -1) {
  console.error(`"${directionId}" is not registered in src/lib/directions.ts — run scripts/new-direction.mjs first.`)
  process.exit(1)
}

const screensMarker = 'screens: ['
const screensIdx = registry.indexOf(screensMarker, directionIdx)
if (screensIdx === -1) {
  console.error(`Couldn't find a "screens: [" array for "${directionId}" in src/lib/directions.ts.`)
  process.exit(1)
}

if (registry.slice(screensIdx, registry.indexOf(']', screensIdx)).includes(`id: '${screenId}'`)) {
  console.error(`Screen "${screenId}" is already registered under "${directionId}".`)
  process.exit(1)
}

function toComponentName(rawId) {
  return rawId
    .split('-')
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join('')
}

mkdirSync(screensDir, { recursive: true })
writeFileSync(
  screenPath,
  `export default function ${toComponentName(directionId)}${toComponentName(screenId)}Screen() {
  return (
    <div
      className="flex min-h-full flex-col items-center justify-center gap-4 p-12 text-center"
      style={{ borderRadius: 'var(--radius)' }}
    >
      <h1
        className="text-[3em] font-bold"
        style={{ color: 'var(--color-accent)', fontFamily: 'var(--font-display)' }}
      >
        ${label}
      </h1>
      <p style={{ color: 'var(--color-muted)', fontFamily: 'var(--font-mono)' }}>
        Fill in from your prompt's Aesthetic/Placement (see PROMPT_TEMPLATE.md). Bind every
        color/font/radius to the CSS custom properties in src/tokens.css, and size type in
        em so the TweakBar's Font Scale slider moves it. Check the design in both themes
        (press D). If it needs an image, generate a real one with scripts/imagegen/index.mjs —
        never leave a placeholder image/icon/stock photo in.
      </p>
    </div>
  )
}
`
)

const closeIdx = registry.indexOf(']', screensIdx)
const before = registry.slice(0, closeIdx).replace(/\s*$/, '')
const entry = `\n      { id: '${screenId}', label: '${label}', component: lazy(() => import('../directions/${directionId}/screens/${screenId}')) },\n    `
registry = before + entry + registry.slice(closeIdx)
writeFileSync(DIRECTIONS_TS, registry)

console.log(`Created ${screenPath}`)
console.log(`Registered "${screenId}" under "${directionId}" in src/lib/directions.ts`)
