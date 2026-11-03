#!/usr/bin/env node
/*
  Inspiration board: collects reference designs BEFORE any direction is built, so the human
  reacts to real pictures instead of a written brief. See AGENTS.md ("Inspiration before
  directions") and USAGE.md step 4.

  Two commands:

    fetch  Search Dribbble for each term, download the thumbnails, optionally screenshot
           public design-system pages, then build the board.
    board  Rebuild the board from inspiration/families.json after you (or the agent) have
           grouped the tiles into style families.

  Usage:
    node scripts/inspiration/index.mjs fetch "admin dashboard" "government website" \
      [--per-term 12] [--url https://example.design/system ...] [--out inspiration]
    node scripts/inspiration/index.mjs board [--out inspiration]
    (or: pnpm inspiration fetch "admin dashboard" ...)

  Output (all inside --out, default ./inspiration, which is gitignored):
    tiles.json     every tile: id, title (the shot's alt text), source term, image, link
    families.json  the grouping you edit; created with one "Unsorted" family if missing
    img/           downloaded thumbnails and screenshots
    index.html     the board: families lettered A, B, C..., tiles numbered A1, A2, B1...

  The images are other people's copyrighted work. They are for looking at during a session
  only; never commit them, never ship them in a direction. That is why --out is gitignored.

  Needs Google Chrome (headless) and curl. Plain HTTP fetches of Dribbble return a bot
  challenge page, so pages are rendered with `chrome --dump-dom`. Override the binary with
  CHROME_PATH. Each Chrome call is killed after a timeout, because some pages never finish.
*/
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const CHROME =
  process.env.CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
/* A normal desktop user agent: the default headless one is served a challenge page. */
const UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36'
const CHROME_TIMEOUT_MS = 60_000
const DOWNLOAD_CONCURRENCY = 8

const [, , command, ...rest] = process.argv

function usage() {
  console.error(
    [
      'Usage:',
      '  node scripts/inspiration/index.mjs fetch "term one" "term two" [--per-term 12] [--url https://...] [--out inspiration]',
      '  node scripts/inspiration/index.mjs board [--out inspiration]',
    ].join('\n'),
  )
  process.exit(1)
}

/* Parse flags; --url may repeat. Everything else that is not a flag value is a term. */
const terms = []
const urls = []
const opts = { out: 'inspiration', 'per-term': '12' }
for (let i = 0; i < rest.length; i++) {
  const a = rest[i]
  if (a === '--url') urls.push(rest[++i])
  else if (a.startsWith('--')) opts[a.slice(2)] = rest[++i]
  else terms.push(a)
}
const OUT = resolve(process.cwd(), opts.out)
const IMG = resolve(OUT, 'img')
const PER_TERM = Number(opts['per-term']) || 12

/* Run a command with a hard timeout. Chrome is started in its own process group so the
   whole tree (renderers, GPU helper) dies with it. Resolves with stdout; never rejects. */
function run(cmd, args, timeoutMs) {
  return new Promise((done) => {
    const child = spawn(cmd, args, { detached: true, stdio: ['ignore', 'pipe', 'ignore'] })
    const chunks = []
    child.stdout.on('data', (c) => chunks.push(c))
    const timer = setTimeout(() => {
      try {
        process.kill(-child.pid, 'SIGKILL')
      } catch {
        /* already gone */
      }
    }, timeoutMs)
    const finish = (code) => {
      clearTimeout(timer)
      done({ code, out: Buffer.concat(chunks).toString('utf8') })
    }
    child.on('close', finish)
    child.on('error', () => finish(-1))
  })
}

function chromeArgs(extra) {
  /* A throwaway profile inside OUT: with the human's real profile in use, Chrome can
     silently hand off to the running instance and produce nothing. */
  return [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--hide-scrollbars',
    `--user-data-dir=${resolve(OUT, '.chrome-profile')}`,
    `--user-agent=${UA}`,
    ...extra,
  ]
}

async function dumpDom(url) {
  const { out } = await run(
    CHROME,
    chromeArgs(['--virtual-time-budget=15000', '--dump-dom', url]),
    CHROME_TIMEOUT_MS,
  )
  return out
}

function decode(s) {
  return s
    .replace(/&nbsp;/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .trim()
}

/* Each search result is an <li id="screenshot-<id>"> holding a <noscript><img src alt>.
   Video shots and promoted slots without an image are skipped. */
function parseShots(html) {
  const shots = []
  for (const chunk of html.split('<li id="screenshot-').slice(1)) {
    const id = chunk.match(/^(\d+)"/)?.[1]
    const img = chunk.match(/<img[^>]*src="(https:\/\/cdn\.dribbble\.com\/userupload\/[^"?]+)/)
    if (!id || !img) continue
    const alt = chunk.match(/<img[^>]*alt="([^"]*)"/)?.[1] ?? ''
    const title = chunk.match(/class="shot-title">\s*([^<]+)</)?.[1] ?? alt
    shots.push({
      id,
      title: decode(title),
      alt: decode(alt),
      src: `${img[1]}?resize=1000x750&vertical=center`,
      ext: (img[1].match(/\.(jpe?g|png|gif|webp)$/i)?.[1] ?? 'jpg').toLowerCase(),
      link: `https://dribbble.com/shots/${id}`,
    })
  }
  return shots
}

async function pool(items, size, fn) {
  let next = 0
  const workers = Array.from({ length: Math.min(size, items.length) }, async () => {
    while (next < items.length) await fn(items[next++])
  })
  await Promise.all(workers)
}

function nonEmpty(path) {
  return existsSync(path) && statSync(path).size > 0
}

async function fetchAll() {
  if (terms.length === 0 && urls.length === 0) usage()
  if (!existsSync(CHROME)) {
    console.error(`Chrome not found at "${CHROME}". Set CHROME_PATH to your Chrome or Chromium binary.`)
    process.exit(1)
  }
  mkdirSync(IMG, { recursive: true })

  const tilesPath = resolve(OUT, 'tiles.json')
  const tiles = existsSync(tilesPath) ? JSON.parse(readFileSync(tilesPath, 'utf8')) : []
  const known = new Set(tiles.map((t) => t.id))

  /* Sequential: concurrent Chrome instances cannot share one profile directory. */
  for (const term of terms) {
    const url = `https://dribbble.com/search/${encodeURIComponent(term)}`
    process.stdout.write(`dribbble: "${term}" ... `)
    const shots = parseShots(await dumpDom(url))
    const fresh = shots.filter((s) => !known.has(s.id)).slice(0, PER_TERM)
    console.log(`${shots.length} found, ${fresh.length} new`)
    if (shots.length === 0) console.log('  (no shots parsed: the page may have changed or served a challenge)')
    for (const s of fresh) {
      known.add(s.id)
      tiles.push({ id: s.id, kind: 'dribbble', term, title: s.title, alt: s.alt, src: s.src, file: `img/${s.id}.${s.ext}`, link: s.link })
    }
  }

  const pending = tiles.filter((t) => t.kind === 'dribbble' && !nonEmpty(resolve(OUT, t.file)))
  console.log(`downloading ${pending.length} thumbnails ...`)
  let ok = 0
  await pool(pending, DOWNLOAD_CONCURRENCY, async (t) => {
    await run('curl', ['-sSfL', '--max-time', '30', '-A', UA, '-o', resolve(OUT, t.file), t.src], 40_000)
    if (nonEmpty(resolve(OUT, t.file))) ok++
    else rmSync(resolve(OUT, t.file), { force: true })
  })
  console.log(`  ${ok}/${pending.length} downloaded`)

  let n = tiles.filter((t) => t.kind === 'site').length
  for (const url of urls) {
    if (tiles.some((t) => t.kind === 'site' && t.link === url)) continue
    const id = `site${++n}`
    const file = `img/${id}.png`
    process.stdout.write(`screenshot: ${url} ... `)
    await run(
      CHROME,
      chromeArgs(['--window-size=1440,900', '--virtual-time-budget=10000', `--screenshot=${resolve(OUT, file)}`, url]),
      CHROME_TIMEOUT_MS,
    )
    const got = nonEmpty(resolve(OUT, file))
    console.log(got ? 'ok' : 'failed')
    if (got) tiles.push({ id, kind: 'site', term: 'design system', title: new URL(url).hostname, alt: url, file, link: url })
  }

  rmSync(resolve(OUT, '.chrome-profile'), { recursive: true, force: true })
  writeFileSync(tilesPath, JSON.stringify(tiles, null, 2) + '\n')
  console.log(`wrote ${tilesPath} (${tiles.length} tiles)`)
  buildBoard()
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
}

/* families.json shape:
   { "title": "Inspiration", "families": [ { "name": "Calm editorial", "note": "why", "tiles": ["25586920", "site1"] } ] }
   Tiles not listed in any family are shown last under "Unsorted". */
function buildBoard() {
  const tilesPath = resolve(OUT, 'tiles.json')
  if (!existsSync(tilesPath)) {
    console.error(`No ${tilesPath}. Run the fetch command first.`)
    process.exit(1)
  }
  const tiles = JSON.parse(readFileSync(tilesPath, 'utf8')).filter((t) => nonEmpty(resolve(OUT, t.file)))
  const byId = new Map(tiles.map((t) => [t.id, t]))

  const famPath = resolve(OUT, 'families.json')
  if (!existsSync(famPath)) {
    writeFileSync(
      famPath,
      JSON.stringify({ title: 'Inspiration', families: [{ name: 'Unsorted', note: 'Group these into 4 to 5 style families.', tiles: tiles.map((t) => t.id) }] }, null, 2) + '\n',
    )
    console.log(`wrote ${famPath} (edit it to group tiles, then run: node scripts/inspiration/index.mjs board)`)
  }
  const spec = JSON.parse(readFileSync(famPath, 'utf8'))
  const families = (spec.families ?? []).map((f) => ({ ...f, tiles: (f.tiles ?? []).filter((id) => byId.has(String(id))).map(String) }))
  const used = new Set(families.flatMap((f) => f.tiles))
  const leftover = tiles.map((t) => t.id).filter((id) => !used.has(id))
  if (leftover.length) families.push({ name: 'Unsorted', note: 'Not yet in a family.', tiles: leftover })

  const key = []
  const sections = families
    .filter((f) => f.tiles.length)
    .map((f, fi) => {
      const letter = String.fromCharCode(65 + fi)
      const cards = f.tiles
        .map((id, ti) => {
          const t = byId.get(id)
          const label = `${letter}${ti + 1}`
          key.push({ label, id, link: t.link })
          return `<a class="tile" href="${escapeHtml(t.link)}" target="_blank" rel="noopener">
  <span class="label">${label}</span>
  <img src="${escapeHtml(t.file)}" alt="${escapeHtml(t.alt || t.title)}" loading="lazy">
  <span class="cap">${escapeHtml(t.title)}<small>${escapeHtml(t.kind === 'site' ? t.link : t.term)}</small></span>
</a>`
        })
        .join('\n')
      return `<section>
<h2><span>${letter}</span> ${escapeHtml(f.name)}</h2>
${f.note ? `<p class="note">${escapeHtml(f.note)}</p>` : ''}
<div class="grid">
${cards}
</div>
</section>`
    })
    .join('\n')

  const title = escapeHtml(spec.title || 'Inspiration')
  const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>
  :root { color-scheme: dark; }
  body { margin: 0; padding: 32px 40px 80px; background: #111214; color: #e8e8ea; font: 15px/1.45 system-ui, sans-serif; }
  header h1 { margin: 0 0 6px; font-size: 28px; }
  header p { margin: 0 0 8px; color: #9a9ca3; max-width: 70ch; }
  section { margin-top: 44px; }
  h2 { font-size: 20px; margin: 0 0 4px; display: flex; gap: 12px; align-items: center; }
  h2 span { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 8px; background: #e8e8ea; color: #111214; }
  .note { margin: 0 0 14px; color: #9a9ca3; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 18px; }
  .tile { position: relative; display: block; color: inherit; text-decoration: none; background: #1b1c1f; border-radius: 10px; overflow: hidden; }
  .tile:hover, .tile:focus-visible { outline: 2px solid #e8e8ea; }
  .tile img { display: block; width: 100%; aspect-ratio: 4 / 3; object-fit: cover; background: #26272b; }
  .label { position: absolute; top: 10px; left: 10px; padding: 3px 9px; border-radius: 6px; background: #111214e6; font-weight: 700; font-size: 16px; }
  .cap { display: block; padding: 10px 12px 12px; font-size: 13px; }
  .cap small { display: block; color: #8a8c93; margin-top: 2px; }
</style>
</head>
<body>
<header>
<h1>${title}</h1>
<p>${tiles.length} references in ${families.filter((f) => f.tiles.length).length} groups. Name the families you like (for example "B and D"), then the tiles (for example "B2, B5"). Each tile links to its source.</p>
<p>Reference only: these images belong to their authors. Do not commit or reuse them.</p>
</header>
${sections}
</body>
</html>
`
  writeFileSync(resolve(OUT, 'index.html'), html)
  writeFileSync(resolve(OUT, 'labels.json'), JSON.stringify(key, null, 2) + '\n')
  console.log(`wrote ${resolve(OUT, 'index.html')} (${tiles.length} tiles)`)
}

if (command === 'fetch') await fetchAll()
else if (command === 'board') buildBoard()
else usage()
