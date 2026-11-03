# Changelog

Format: version, date, then what changed and why (link a `DECISIONS.md` entry when one
exists). Newest first.

## 0.7.1 - 2026-11-01

- **Fix: `codex` generation saved nothing.** The `codex` provider runs `codex exec` inside
  the project, where `AGENTS.md` says never generate without asking the human, so codex
  answered "please confirm" and the script failed with "codex reported success but ...
  does not exist". New flag `--approved` on `scripts/imagegen/index.mjs`: the codex
  prompt then states that the human approved this exact batch. Pass it only with a
  recorded yes from the human. Without the flag nothing changes, except that the error
  now says codex probably stopped to ask and names `--approved`. Other providers ignore
  the flag. Documented in `AGENTS.md`, `README.md`, `PROMPT_TEMPLATE.md` and `USAGE.md`.

## 0.7.0 - 2026-11-01

- **New step 0: inspiration before any direction.** New constraint in `AGENTS.md`
  (mirrored in `CLAUDE.md` and `CONTRIBUTING.md`): no direction is built until the human
  has seen an inspiration board and said which style families and tiles they like.
  Directions are then built from those picks, each `sub` label naming its source tiles.
- **New script: `scripts/inspiration/index.mjs`** (`pnpm inspiration`). `fetch` renders
  Dribbble search pages for the given terms in local headless Chrome, downloads the
  thumbnails in parallel with curl, optionally screenshots public design-system pages
  (`--url`), and builds the board; `board` rebuilds it from `inspiration/families.json`,
  where tiles are grouped into families and numbered A1, A2, B1... No new dependencies.
- `inspiration/` is gitignored: the images are other people's copyrighted work, for
  viewing during a session only.
- `PROMPT_TEMPLATE.md` gains a "Step 0: Inspiration" section; `USAGE.md` gains section 4
  (later sections renumbered, cross-references updated); `README.md` gains an
  "Inspiration first" section.
- See `DECISIONS.md` 2026-11-01 "Inspiration board before any direction".

## 0.6.1 - 2026-08-27

- **New constraint: no generation without asking.** An agent must ask the human before
  running image or video generation, for *every* provider - `codex` spends a ChatGPT Plus
  session, `openai` bills per image, and `local` occupies a GPU. One confirmation covers
  one stated batch, never a session. Agents must also never select or switch a paid
  provider on their own initiative. Previously nothing said this, and 0.6.0's "a direction
  is not done until it carries real generated imagery" actively read as a licence to
  generate unprompted.
- **New constraint: never guess where a local generation server lives.** If
  `IMAGE_GEN_BASE_URL` is unset, ask - no port scanning, no assumed default host, no
  hunting the filesystem for a model directory. A fresh clone on an unfamiliar machine
  cannot know the address, and neither necessarily does the human off the top of their
  head. `local.mjs`'s error message now says so directly.
- **New constraint: a direction must make a deliberate motion decision.** Either it moves,
  with every transition built from `--motion-duration` / `--motion-ease` /
  `--reveal-distance` and gated by `--motion-on`, or its stillness is a stated aesthetic
  choice. A hardcoded `transition: 200ms ease` is the motion equivalent of a hardcoded hex
  and makes the TweakBar's Motion group a lie.
- `--motion-on` now defaults to `0` under `prefers-reduced-motion: reduce`, so any
  direction that gates motion properly honours the OS setting for free. The TweakBar can
  still switch it back on - the preference is the default, not a lock.
- `scripts/imagegen/index.mjs` carries the consent rule in its header comment, and the
  `openai` provider prints a "this call bills your API account" line on every run.
- Rules mirrored into `AGENTS.md`, `CLAUDE.md` (subagent brief), `CONTRIBUTING.md`,
  `README.md`, `PROMPT_TEMPLATE.md`, `src/directions/README.md` and `.env.example`.
- See `DECISIONS.md` 2026-08-27 "Generation requires consent; motion is a required
  decision".

## 0.6.0 - 2026-08-27

- **Breaking (token shape):** `src/tokens.css` roughly tripled. New tokens:
  `--color-elevated`, `--color-accent-fg`, `--font-body`, `--weight-display`,
  `--weight-body`, `--tracking-display`, `--tracking-body`, `--leading-display`,
  `--leading-body`, `--measure`, `--border-width`, `--pad`, `--shadow-strength`,
  `--shadow-y`, `--shadow`, `--motion-ease`. `--font-serif` was **removed** - the shared
  `--font-display` is now the serif, and a direction differentiates itself by which of the
  three faces it leans on rather than by owning a private one.
- TweakBar went from 6 controls to ~30, in five collapsible groups, with two new field
  types: colour pickers (with a hex field) and selects (font stacks, easing curves).
- TweakBar colour edits are now tracked **per theme**. The panel writes inline styles on
  `<html>`, which beat both `:root` and the light block, so a colour tuned in light mode
  used to leak into dark. "Copy CSS" now exports both blocks.
- Image generation is now pluggable: `scripts/imagegen.mjs` became
  `scripts/imagegen/index.mjs` plus `providers/{codex,local,openai}.mjs`, dispatched on
  `IMAGE_GEN_PROVIDER` (default `codex`, unchanged behaviour). The `local` provider talks
  to a self-hosted generation server configured only via `IMAGE_GEN_BASE_URL` - no host
  path is committed.
- The asset rule is now a positive requirement: a direction is not done until it carries
  real generated imagery. Avoiding imagery entirely now fails the rule the same way a grey
  placeholder does.
- The three sample directions were rebuilt on the full token set, so the new sliders
  visibly move them.
- Fixed a React effect-ordering bug surfaced while verifying the above: child effects run
  before parent ones, so TweakBar's effect fired with the new `theme` prop while `useTheme`
  (in `App`) had not yet written `data-theme` to `<html>`. Every computed read in that
  effect therefore returned the previous theme's values - the panel showed stale numbers
  and "Copy CSS" wrote them into the wrong block. TweakBar now sets the attribute itself
  before reading.
- Removed the last references to the maintainer's private planning notes from
  `scripts/videogen/*`.
- See `DECISIONS.md` 2026-08-27 "Rich token set, per-theme tuning, assets as a
  requirement", issue #12.

## 0.5.0 - 2026-08-27

- **Breaking (token shape):** `src/tokens.css` gained a `:root[data-theme='light']`
  override block and a separate `--chrome-*` token set for the harness's own UI. A
  direction carried over from 0.4.x still renders, but any color it defines only in
  `:root` will keep its dark value in light mode.
- Added a light/dark toggle in the top control bar (`D`), deliberately outside the
  rendered screen so it never becomes part of the design under review. Choice persists in
  `localStorage`.
- Moved the screen switcher from a horizontal bar under the tabs to a left sidebar,
  navigated with `↑`/`↓`. `←`/`→` now step through directions. All shortcuts moved out of
  `DirectionToggle` into one handler, `src/lib/useKeyboardNav.ts`.
- Added three sample directions (Editorial, Brutalist, Soft), three screens each, marked
  `sample: true` and badged in the tab bar. `scripts/remove-samples.mjs` deletes every
  sample and scaffolds a blank `v1`.
- Fixed: the TweakBar's Font Scale slider did nothing - nothing read `--font-scale`.
  `DeviceFrame` now derives the preview's base font size from it, so directions must size
  type in `em`.
- Fixed: TweakBar held authored CSS ("8px") in state rather than a bare number, so every
  px field rendered as "8pxpx" and fed an invalid value to its `<input type="range">`,
  which silently fell back to the slider's midpoint. Units are now re-attached on write,
  display, and export.
- See `DECISIONS.md` 2026-08-27 "Left screen rail, arrow-key nav, light theme, shipped
  samples", issue #11.

## 0.4.0 - 2026-08-26

- Renamed `design-template` -> `ui-design-lab` (repo, package name, docs). Open-sourced
  under MIT: added `LICENSE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`,
  bug/feature issue templates, PR template, and a CI workflow (`pnpm lint` + `pnpm
  build` on push/PR to `master`). Rewrote `README.md` for search/answer-engine
  discoverability (keyword-led intro, feature table, FAQ, doc map). Replaced the default
  Vite favicon and page title (closes #4). See `DECISIONS.md` 2026-08-26 "Renamed to
  `ui-design-lab`, went public under MIT", issue #8.

## 0.3.0 - 2026-08-17

- Added multi-screen support per direction (`Direction.screens: Screen[]`, breaking
  change to `src/lib/directions.ts`'s shape), `ScreenNav` component, and
  `scripts/new-screen.mjs`. Added a filter input to `DirectionToggle`'s tab bar (shown
  past 6 directions). See `DECISIONS.md` 2026-08-17 "Multi-screen directions + direction
  filter", issue #7.

## 0.2.1 - 2026-08-17

- Added a hard "never use placeholder images/assets" constraint (generate real ones via
  `scripts/imagegen.mjs` instead). Updated `AGENTS.md`, `README.md`, `USAGE.md`, and the
  stub generated by `scripts/new-direction.mjs`. See `DECISIONS.md` 2026-08-17 "No
  placeholder images/assets in directions".

## 0.2.0 - 2026-08-17

- Switched to pnpm (`packageManager` field, `preinstall` guard blocking npm/yarn,
  `pnpm-lock.yaml` replacing `package-lock.json`). Deleted `HANDOFF.md`, moved its open
  items to GitHub issues #1-#5. Rewrote `README.md` for accuracy. See `DECISIONS.md`
  2026-08-17 "Switch to pnpm, retire HANDOFF.md in favor of issues".

## 0.1.0 - 2026-08-17

- Added `AGENTS.md`, `CLAUDE.md` shim, `DECISIONS.md`, this changelog, and versioning
  conventions. No functional app code changed. Bumped from `0.0.0` because the harness
  is functional (builds/typechecks clean) even though `HANDOFF.md` lists unverified
  items - see `DECISIONS.md` 2026-08-17 "Agent docs, versioning, no tracker".
