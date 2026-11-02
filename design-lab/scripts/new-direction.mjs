#!/usr/bin/env node
/*
  Scaffolds a new direction: creates src/directions/<id>/Page.tsx (registered as its
  "home" screen) and registers the direction in src/lib/directions.ts, so adding a
  direction is one command instead of the manual mkdir + touch + hand-edit-the-registry
  flow described in USAGE.md section 6. To add more screens to a direction afterward,
  use scripts/new-screen.mjs.

  Usage:
    node scripts/new-direction.mjs <id> --label "V2" --sub "Short description" [--key 2]

  If --key is omitted, it defaults to the next unused digit (1-9).
*/
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const DIRECTIONS_TS = resolve(ROOT, 'src/lib/directions.ts')

const [, , id, ...rest] = process.argv

function usage() {
  console.error('Usage: node scripts/new-direction.mjs <id> --label "V2" --sub "Short description" [--key 2]')
  process.exit(1)
}

if (!id || id.startsWith('--')) usage()
if (!/^[a-z0-9-]+$/.test(id)) {
  console.error(`Invalid id "${id}" — use lowercase letters, digits, hyphens (e.g. v2, v3a).`)
  process.exit(1)
}

function flag(name, fallback) {
  const i = rest.indexOf(`--${name}`)
  return i === -1 ? fallback : rest[i + 1]
}

const label = flag('label', id.toUpperCase())
const sub = flag('sub', '')

const dirPath = resolve(ROOT, `src/directions/${id}`)
const pagePath = resolve(dirPath, 'Page.tsx')

if (existsSync(pagePath)) {
  console.error(`${pagePath} already exists — pick a different id or edit it directly.`)
  process.exit(1)
}

let registry = readFileSync(DIRECTIONS_TS, 'utf8')

if (registry.includes(`id: '${id}'`)) {
  console.error(`"${id}" is already registered in src/lib/directions.ts.`)
  process.exit(1)
}

let key = flag('key', undefined)
if (!key) {
  const usedKeys = new Set([...registry.matchAll(/key:\s*'(\d)'/g)].map((m) => m[1]))
  key = '123456789'.split('').find((k) => !usedKeys.has(k))
  if (!key) {
    console.error('No unused number key 1-9 left — pass --key explicitly (shortcuts stop at 9).')
    process.exit(1)
  }
}

mkdirSync(dirPath, { recursive: true })
writeFileSync(
  pagePath,
  `export default function ${toComponentName(id)}Page() {
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

function toComponentName(rawId) {
  return rawId
    .split('-')
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join('')
}

const entry = `  {
    id: '${id}',
    key: '${key}',
    label: '${label}',
    sub: '${sub}',
    screens: [
      { id: 'home', label: 'Home', component: lazy(() => import('../directions/${id}/Page')) },
    ],
  },
`

registry = registry.replace(/(export const directions: Direction\[\] = \[\n)/, `$1${entry}`)
writeFileSync(DIRECTIONS_TS, registry)

console.log(`Created ${pagePath}`)
console.log(`Registered "${id}" in src/lib/directions.ts (key: ${key})`)
