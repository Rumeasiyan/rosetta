# AGENTS.md

Working reference for AI agents (and new humans) in this repo. Read this before making
changes. Open work items live as GitHub issues (`gh issue list`), not in a doc.

## What this project is

`ui-design-lab` is a reusable scaffold, cloned per UI/UX design project, for running
multiple aesthetic "directions" side-by-side with a live tweak bar for design tokens,
device-size preview frames, and scripted image/video generation. It is not itself a
client project - it's the harness other projects get built inside of. Stack: Vite +
React 19 + TypeScript + Tailwind v4 (`@tailwindcss/vite`).

The original spec and the research trail behind the image/video-gen tooling choices live
in the maintainer's private planning notes, outside this repo. Everything load-bearing
that came out of them is recorded in `DECISIONS.md` - read that rather than hunting for
the notes.

## Where to look

| Area | When you need it |
|---|---|
| `src/tokens.css` | Any visual value - colors, fonts, radius, gap, motion. Single source of design truth. Also holds the `--chrome-*` set for the harness UI and the light-theme override block. |
| `src/directions/<id>/Page.tsx` | A direction's default ("home") screen. |
| `src/directions/<id>/screens/<screenId>.tsx` | Additional screens within a direction. |
| `src/lib/directions.ts` | Registering a direction and its `screens` array (id, number-key shortcut, lazy imports). |
| `src/components/DirectionToggle.tsx` | Direction tab bar, SAMPLE badges, filter input once there are >6 directions. Shortcuts are NOT here - see `useKeyboardNav.ts`. |
| `src/components/ScreenNav.tsx` | Left sidebar listing a direction's screens (only renders when a direction has >1 screen). |
| `src/lib/useKeyboardNav.ts` | Every keyboard shortcut. One handler - directions and screens share the arrow keys. |
| `src/lib/theme.ts` + `src/components/ThemeToggle.tsx` | Light/dark state and its top-bar control. |
| `src/directions/README.md` | What the shipped sample directions are and how to delete them. |
| `src/components/DeviceFrame.tsx` | Mobile/tablet/desktop preview frame widths. |
| `src/components/TweakBar.tsx` | Live token tuning UI; `FIELDS` list must match `tokens.css`. |
| `PROMPT_TEMPLATE.md` | Starting a design session - the Aesthetic/Reference/Intent/Guardrails pattern + worked example. |
| `scripts/inspiration/index.mjs` | Step 0 of a session: fetch Dribbble references and design-system screenshots into a gitignored `inspiration/` board, grouped by the families in `inspiration/families.json`. Run as `pnpm inspiration fetch "term" ...` then `pnpm inspiration board`. |
| `scripts/new-direction.mjs` | Scaffolding a new direction (folder + `Page.tsx` "home" screen + registry entry) in one command. |
| `scripts/new-screen.mjs` | Scaffolding an additional screen into an existing direction. |
| `scripts/remove-samples.mjs` | Deleting the shipped sample directions (everything marked `sample: true`). |
| `scripts/imagegen/index.mjs` + `providers/*.mjs` | Image generation dispatcher: `codex` (default, no API key), `local` (self-hosted server), `openai` (paid). |
| `src/components/TweakBar.tsx` `FIELDS` | The tunable-token list. Colour fields are per-theme; see the header comment before touching it. |
| `scripts/videogen/index.mjs` + `providers/*.mjs` | Video generation dispatcher + per-provider HTTP calls. |
| `USAGE.md` | Step-by-step walkthrough of running a design session (clone, tabs, tuning, generating assets). |
| `CONTRIBUTING.md` | What an outside contributor must satisfy before opening a PR (mirrors the Constraints below). |
| `.github/workflows/ci.yml` | CI: runs `pnpm lint` + `pnpm build` on every push/PR to `master`. |
| `gh issue list --label unverified` | Anything flagged as needing real-world verification before you trust it. |

## Working agreements

- Repo: `github.com/Rumeasiyan/ui-design-lab` - **public, MIT licensed, open to outside
  contributors.** `gh` CLI is authenticated as `Rumeasiyan`. Issues are used as a task tracker: open one before starting non-trivial
  work, assign to `Rumeasiyan` (solo repo - no one else to assign to), work, commit
  referencing the issue number, comment the outcome, close.
  - Closing comment states: what was built, what was verified, the resulting
    `package.json` version if it changed, and anything deliberately deferred (link the
    follow-up issue).
  - Skip an issue for trivial fixes with no ambiguity - typo, formatting, a one-line
    obviously-correct change. If you're explaining "why" to a future reader, it's not
    trivial.
  - Template: `.github/ISSUE_TEMPLATE/task.md`. Labels beyond GitHub's defaults:
    `harness` (core scaffold), `video-provider` (touches the unverified paid providers),
    `unverified` (needs real browser/API verification before trusted).
- Anything a future reader would need - an open question, a deferred fix, a discovered
  bug, a risky assumption - becomes an issue (or a `DECISIONS.md` entry for a resolved
  decision) at the moment it's found, not left only in conversation.
- Branch policy: the maintainer commits directly to `master` - feature branches for
  solo work would just be ceremony. Outside contributors fork and open a PR; CI
  (`pnpm lint` + `pnpm build`) must be green before merge.
- Public-repo hygiene: this repo is read by strangers. Keep `README.md` accurate to what
  actually works (an unverified feature is labeled unverified, not implied working), and
  never let a private path, key, or client name land in a commit.

## Constraints

- **No direction is built until the human has seen an inspiration board and said what
  they like.** Before scaffolding any direction, collect references with
  `scripts/inspiration/index.mjs` (search terms from the product's domain and users, not
  from style words), group them into 4 to 5 style families with numbered tiles, ask which
  families the human likes, then which tiles within a family that mixes styles, and only
  then build 2 to 3 directions from the picks, each direction's `sub` label naming its
  source tiles (e.g. "Calm editorial (A1, A4)"). Why: directions built straight from a
  written brief get rejected wholesale, wasting the most expensive part of the session;
  people recognise what they like far more reliably than they can describe it, and a
  board plus one narrowing question converges in about two rounds. Full steps in
  `USAGE.md` section 4. Enforced by convention.
- **Never commit the `inspiration/` folder, and never put a reference image into a
  direction.** The images are other people's copyrighted work, fetched for viewing only.
  `inspiration/` is gitignored; keep it that way. Match a reference's feel, never copy or
  trace it.
- **`tokens.css` is the single source of design truth.** Every color/font/radius/spacing
  a direction uses must be a CSS custom property there, never hardcoded in a component.
  Why: the TweakBar and "Copy CSS" export only work if every visual value is a token.
  Enforced by convention only (no lint rule) - check by eye when reviewing a direction.
- **Each direction fully commits to its own aesthetic; never blend two directions in one
  component.** Why: the point of the harness is side-by-side comparison of distinct
  directions - blending defeats that. Enforced by convention (`src/lib/directions.ts`
  comment, `README.md`).
- **The harness ships three `sample: true` directions; they are examples, not content.**
  Never build on top of one, and never mark a real direction `sample: true` -
  `scripts/remove-samples.mjs` deletes every entry carrying that flag, folder and all.
  Why: users need to see a finished direction before writing one, but a sample left in the
  tab bar next to real work ruins the comparison. Enforced by the SAMPLE badge in
  `DirectionToggle` and the removal script.
- **A direction never references a `--chrome-*` token, and the harness UI never uses a
  `--color-*` one.** Why: the chrome is the tool, the `--color-*` set is the design under
  review; mixing them means tuning a direction restyles the toolbar. Enforced by
  convention - check by eye.
- **Type in a direction is sized in `em`, never `rem`/`px`.** Why: `DeviceFrame` sets the
  preview's base font-size from `--font-scale`; a `rem` resolves against the document root
  and silently ignores the Font Scale slider. Enforced by convention.
- **Every color token needs a value in the `:root[data-theme='light']` block too.** Why: a
  token defined only in `:root` keeps its dark value in light mode, which usually reads as
  an invisible or blown-out element rather than an obvious bug. Enforced by convention -
  press `D` and look.
- **A direction is not done until it carries real generated imagery.** This is a positive
  requirement, not only a ban on placeholders. Three things are equally disallowed: a grey
  placeholder box, stock art, and a design that quietly avoids imagery so it never has to
  source any. Why: a screen built purely from type, borders and flat colour reads as
  machine-generated, and a review that can't tell "restrained" from "unfinished" is
  worthless - which defeats the entire point of comparing directions. Generate assets with
  `scripts/imagegen/index.mjs` - but **ask the human first**, see the generation-consent
  rule below. "Required" means the direction is not finished until the assets exist; it
  does not mean generate them unprompted. Enforced by convention only - check by eye when
  reviewing a direction.
- **Never start image or video generation without asking the human first, for every
  provider.** Propose the prompts, the target paths and the provider you intend to use,
  then wait for a yes. One confirmation covers one stated batch ("these four assets") -
  it is not standing approval for the session, and it never carries over to a different
  provider. Once you have that yes, run `scripts/imagegen/index.mjs` with `--approved`:
  `codex` runs inside this project, reads this same rule, and otherwise stops to ask and
  saves nothing. Never pass `--approved` without a recorded yes. Why: every provider costs the human something they can see on a bill or a
  machine. `codex` spends their ChatGPT Plus session, `openai` spends per image, and even
  `local` occupies their GPU for minutes at a time. An agent that reads "imagery is
  required" and starts generating has spent someone else's money to satisfy a doc.
- **Never select or switch a paid provider on your own initiative.** `IMAGE_GEN_PROVIDER`
  and `VIDEO_GEN_PROVIDER` are `.env` decisions the human makes. Proposing "this needs the
  openai provider, shall I?" is fine; setting it is not.
- **Never guess, probe or hardcode where a local generation server lives.** If
  `IMAGE_GEN_BASE_URL` is unset, ask - do not scan ports, do not assume a default host, do
  not go looking through the filesystem for a model directory. Why: a fresh clone on an
  unfamiliar machine has no way to know this, and the human may not know the address off
  the top of their head either. Asking is the only correct move. `local.mjs` already fails
  with a clear message rather than falling back to a guess; keep it that way.
- **Show, don't tell: a screen is read by looking, not by reading.** Every screen's main
  question ("what needs me now?", "where is this order?", "is this paid?") must be
  answered by a visual of the real-world thing it is about, before any number, table or
  chart: a map with the places on it, a route with the item travelling along it, a
  document or stamp that changes state, a parcel or building filled to its value, two
  things connecting when they match, a stack that grows or empties. Numbers and text
  support the visual; they never replace it. Moving parts show change (progress travels,
  matches snap together, a stamp lands, a queue drains), built from the motion tokens and
  gated by `--motion-on`. A plain grid of counters, a bar chart or a data table as the
  hero of a screen fails review. Why: most people using real products are not analysts;
  they understand a picture of their work at a glance and learn it without training, and
  a screen that has to be read is a screen that gets misread. Accessibility still holds:
  every visual carries a text equivalent (`<title>`, caption, or visually hidden text)
  and colour is never the only signal. Enforced by convention: cover the text with your
  hand and check the state is still obvious.
- **A direction must make a deliberate motion decision, expressed in tokens.** Either it
  moves - and every transition derives from `--motion-duration`, `--motion-ease` and
  `--reveal-distance`, gated by `--motion-on` so the TweakBar toggle genuinely stops it -
  or motion is deliberately absent and that absence is the aesthetic choice. What fails is
  a direction that simply never considered it. Why: a completely inert interface reads as
  a screenshot, the same way a direction with no imagery reads as machine-generated, and
  neither can be judged for feel. A hardcoded `transition: 200ms ease` is the motion
  equivalent of a hardcoded hex - it makes the Motion group of the TweakBar a lie.
  Enforced by convention - flip Motion off in the TweakBar and confirm the direction
  actually stops moving.
- **`VIDEO_GEN_PROVIDER` defaults to `manual` (no API call, no cost).** Why: explicit
  user decision - pay-per-use video APIs are opt-in per project, not on by default. Do
  not change the default. See `scripts/videogen/providers/manual.mjs`.
- **`IMAGE_GEN_PROVIDER` defaults to `codex`** (no API key, reuses the ChatGPT Plus
  session). The `local` provider is configured only through `IMAGE_GEN_BASE_URL` in `.env`
  - **never commit a host path or machine-specific location**; the repo is public and that
  URL is the user's, not the template's.
- **TweakBar colour edits are per-theme.** The panel writes to
  `document.documentElement.style`, which is inline and beats both `:root` and
  `:root[data-theme='light']`. Anything `tokens.css` defines in its light block must be
  marked `themed: true` in `FIELDS`, or tuning it in one theme silently leaks into the
  other. Enforced by convention - the mechanism is documented in `TweakBar.tsx`'s header.
- **The three paid video providers (`replicate.mjs`, `openrouter.mjs`,
  `atlascloud.mjs`) are unverified guesses**, written from search-result summaries, not
  each provider's real API reference. Don't trust their request/response shapes without
  checking live docs first - issue #3 has the doc links.
- **`.env` is gitignored; never commit real API keys.** `.env.example` documents the
  shape only. The repo is public - a committed key is a leaked key; rotate first, then
  rewrite history.

## Commands

```bash
pnpm install      # install deps (npm/yarn blocked by preinstall guard - pnpm only)
pnpm dev          # vite dev server
pnpm build        # tsc -b && vite build
pnpm lint         # oxlint (rules: react/rules-of-hooks=error, react/only-export-components=warn)
pnpm preview      # preview a production build
```

CI runs `pnpm install --frozen-lockfile`, `pnpm lint`, and `pnpm build` on every push and
PR to `master` (`.github/workflows/ci.yml`). Run the last two locally before pushing.

No test suite exists yet.

## Conventions

- TypeScript + React function components, one direction per folder under
  `src/directions/<id>/`. A direction has one or more screens (`Screen[]` in
  `src/lib/directions.ts`), each lazy-loaded via `React.lazy`; `screens[0]` is
  conventionally `Page.tsx`, additional screens live under `screens/<screenId>.tsx`.
- Number-key shortcuts (1-9) and `←`/`→` select a *direction*; `↑`/`↓` move between the
  active direction's screens via the `ScreenNav` sidebar, which only renders when it has
  more than one. All shortcuts live in `src/lib/useKeyboardNav.ts` - do not add a second
  `keydown` listener elsewhere, and keep the INPUT/TEXTAREA guard: TweakBar's sliders are
  `<input type="range">`, which the arrow keys drive.
- New tunable design values go in **both** `src/tokens.css` (as a custom property) and
  `TweakBar.tsx`'s `FIELDS` list - adding to only one breaks the tuning UI or the export.
- Commit messages: clear, imperative subject lines; body only when the "why" isn't
  obvious from the diff. Reference the issue number when one exists.

## Versioning

- Canonical source: `package.json` `version` field. No separate build number (this is a
  web app served/built via Vite, not a platform with its own build-number concept).
- Current version: `0.7.1` (pre-1.0, template still stabilizing; see open `unverified`
  issues).
- Update **per completed change** (not batched at release time), per semver:
  - Breaking change to the harness (e.g. token-shape change that breaks existing
    directions) → bump `MINOR` while pre-1.0 (see note below), reset `PATCH`.
  - New capability (new direction registry feature, new script, new provider wired up
    for real) → bump `MINOR`, reset `PATCH`.
  - Bug fix, doc fix with no behavior change is skipped, verified-provider fix, small
    tweak → bump `PATCH`.
  - Docs, comments, formatting, refactor with no user-visible behavior change → no
    version bump.
  - **Pre-1.0 note:** per semver, `0.x` releases may include breaking changes in `MINOR`
    bumps. Once the harness is verified end-to-end (all `unverified`-labeled issues
    closed), cut `1.0.0` and switch to standard `MAJOR`-for-breaking rules.
- No build-only artifacts are distributed from this repo (it's cloned, not published), so
  there's no build-number increment step.
- Log every version-affecting change in `CHANGELOG.md`.

## Workflow

1. Check open GitHub issues (`gh issue list`) before starting; check this file's
   Constraints section for anything the change touches.
2. If non-trivial, open an issue (or confirm one already exists) before starting.
3. Make the change.
4. Determine whether it needs a version bump (see Versioning above); if so, update
   `package.json` and add a `CHANGELOG.md` entry.
5. If the change is a decision worth remembering later ("why is it like this?") -
   architecture choice, rejected alternative, resolved open question - add an entry to
   `DECISIONS.md`.
6. Run `pnpm build` and `pnpm lint`; fix failures.
7. Commit directly to `master` referencing the issue number if one exists; comment the
   outcome on the issue and close it.
