#!/usr/bin/env node
/*
  Deletes every direction marked `sample: true` — the example directions that ship with
  the ui-design-lab template — and their registry entries, then scaffolds a blank `v1` if
  that leaves you with nothing.

  Run this once, before you start designing. Leaving a sample in the tab bar next to your
  real work makes the side-by-side comparison meaningless, which is the point of the
  harness.

  Usage:
    node scripts/remove-samples.mjs [--dry-run]

  Directions you created yourself are never touched — only entries carrying `sample: true`.
*/
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const DIRECTIONS_TS = resolve(ROOT, 'src/lib/directions.ts')
const DIRECTIONS_README = resolve(ROOT, 'src/directions/README.md')

const dryRun = process.argv.includes('--dry-run')

const source = readFileSync(DIRECTIONS_TS, 'utf8')

const ARRAY_OPEN = 'export const directions: Direction[] = ['
const openIndex = source.indexOf(ARRAY_OPEN)
if (openIndex === -1) {
  console.error(`Could not find "${ARRAY_OPEN}" in src/lib/directions.ts — has it been restructured?`)
  process.exit(1)
}

const bodyStart = openIndex + ARRAY_OPEN.length
const bodyEnd = source.lastIndexOf(']')
if (bodyEnd <= bodyStart) {
  console.error('Could not find the end of the directions array in src/lib/directions.ts.')
  process.exit(1)
}

const body = source.slice(bodyStart, bodyEnd)

/*
  Entries are written by scripts/new-direction.mjs at a consistent two-space indent, so a
  top-level entry is everything from a line that is exactly "  {" through the matching
  line "  },". Anything nested is indented further and can't match.
*/
const entries = body.match(/^ {2}\{\n[\s\S]*?^ {2}\},\n/gm) ?? []
if (entries.length === 0) {
  console.log('No direction entries found — nothing to remove.')
  process.exit(0)
}

const samples = entries.filter((e) => /\bsample:\s*true\b/.test(e))
if (samples.length === 0) {
  console.log('No sample directions left. Nothing to do.')
  process.exit(0)
}

const sampleIds = samples.map((e) => e.match(/id:\s*'([^']+)'/)?.[1]).filter(Boolean)
const kept = entries.filter((e) => !samples.includes(e))

console.log(`Removing ${samples.length} sample direction(s): ${sampleIds.join(', ')}`)
if (dryRun) {
  console.log('--dry-run: no files changed.')
  process.exit(0)
}

for (const id of sampleIds) {
  const dir = resolve(ROOT, 'src/directions', id)
  if (existsSync(dir)) {
    rmSync(dir, { recursive: true, force: true })
    console.log(`  deleted src/directions/${id}/`)
  }
}

// Drop the "these entries are samples, delete them" note from the registry's header comment.
let header = source.slice(0, openIndex)
header = header.replace(
  /\n\n {2}─{10,}\n {2}The three `sample: true` entries[\s\S]*?─{10,}\n/,
  '\n',
)

writeFileSync(DIRECTIONS_TS, `${header}${ARRAY_OPEN}\n${kept.join('')}${source.slice(bodyEnd)}`)
console.log('  updated src/lib/directions.ts')

if (existsSync(DIRECTIONS_README)) {
  const readme = readFileSync(DIRECTIONS_README, 'utf8')
  const trimmed = readme.replace(/<!-- SAMPLES:START -->[\s\S]*?<!-- SAMPLES:END -->\n\n?/, '')
  if (trimmed !== readme) {
    writeFileSync(DIRECTIONS_README, trimmed)
    console.log('  updated src/directions/README.md')
  }
}

if (kept.length === 0) {
  console.log('\nNo directions left — scaffolding a blank v1.')
  const result = spawnSync(
    process.execPath,
    [resolve(ROOT, 'scripts/new-direction.mjs'), 'v1', '--label', 'V1', '--sub', 'First direction'],
    { stdio: 'inherit' },
  )
  if (result.status !== 0) {
    console.error('Failed to scaffold v1 — run: node scripts/new-direction.mjs v1 --label "V1"')
    process.exit(1)
  }
}

console.log('\nDone. Run `pnpm dev` and start designing.')
