# UI Design Lab

**Compare multiple UI design directions side by side, in the browser, with live design-token tuning.**

UI Design Lab is an open-source **UI/UX design harness**: a Vite + React 19 + Tailwind v4
template you clone per project to explore several distinct aesthetic *directions* for the
same screen at once - each direction fully committed to its own look - then tune the
shared design tokens (color, type, radius, spacing, motion) live with sliders and copy
the result straight back into CSS.

It is built for **AI-assisted design sessions**: you prompt an agent for N directions,
render them in the same harness, judge them side by side, and tune the winner by eye
instead of guessing values in a text editor.

[![CI](https://github.com/Rumeasiyan/ui-design-lab/actions/workflows/ci.yml/badge.svg)](https://github.com/Rumeasiyan/ui-design-lab/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![React 19](https://img.shields.io/badge/React-19-61dafb.svg)](https://react.dev)
[![Vite](https://img.shields.io/badge/Vite-8-646cff.svg)](https://vite.dev)
[![Tailwind CSS v4](https://img.shields.io/badge/Tailwind_CSS-v4-38bdf8.svg)](https://tailwindcss.com)
[![PRs welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

---

## Contents

- [What it does](#what-it-does)
- [Who it's for](#who-its-for)
- [Quick start](#quick-start)
- [How it's organized](#how-its-organized)
- [Inspiration first](#inspiration-first)
- [Adding a new direction](#adding-a-new-direction)
- [Adding a screen to a direction](#adding-a-screen-to-a-direction)
- [Assets](#assets)
- [Motion](#motion)
- [Visuals first](#visuals-first)
- [Video generation](#video-generation)
- [FAQ](#faq)
- [Project status](#project-status)
- [Contributing](#contributing)
- [License](#license)

## What it does

| Feature | What you get |
|---|---|
| **Side-by-side design directions** | One folder per aesthetic direction. Switch with a tab bar, number keys `1`-`9`, `←`/`→`, or press `G` for a grid showing every direction at once. |
| **Three worked samples to start from** | Editorial, Brutalist, and Soft directions - three screens each, built from the same tokens - showing what a finished direction looks like. One command deletes them all when you're ready to design. |
| **Live design-token tuning** | ~30 controls over the real CSS custom properties in `src/tokens.css`: the full palette (colour pickers, per theme), typography (face, weight, tracking, leading, scale, measure), shape, depth, and motion. "Copy CSS" exports both theme blocks. |
| **Device preview frames** | Render any screen at mobile (390px), tablet (834px), or desktop width without resizing your browser. |
| **Light / dark preview** | Every color token has a light-theme value. Toggle with `D` - the control sits in the harness's top bar, never inside the design being judged. |
| **Multi-screen flows** | A direction can hold several screens (Home → Detail → Settings), listed in a left sidebar and stepped through with `↑`/`↓`. |
| **Motion as a first-class token** | Duration, easing, and reveal distance are tokens; `--motion-on` gates everything and defaults off under `prefers-reduced-motion`. |
| **Real assets, not placeholders** | Pluggable image generation - `codex exec` (no API key), a self-hosted local server (no per-image cost), or the OpenAI Images API. A direction is not done until it carries real imagery. |
| **Scripted video generation** | `scripts/videogen/` with a pluggable provider dispatcher. Defaults to `manual`: no API call, no cost. |
| **Inspiration before design** | `pnpm inspiration` pulls Dribbble references and design-system screenshots into a local board of numbered tiles grouped by style family, so the human picks what they like before any direction is built. |
| **A prompt pattern that works** | `PROMPT_TEMPLATE.md` - the Aesthetic / Reference / Intent / Guardrails structure for driving an AI agent to produce genuinely distinct directions, plus a worked example. |

## Who it's for

- **Designers and design engineers** who want to see three or five real, interactive
  takes on a screen instead of static mockups.
- **Anyone doing AI-assisted or "vibe coded" UI work** who needs a repeatable harness so
  each session doesn't start by rebuilding the same scaffolding.
- **Teams making a design-direction decision** and wanting to compare candidates under
  identical content and identical device widths.

## Requirements

- Node.js (22+ recommended)
- [pnpm](https://pnpm.io) - this repo is **pnpm-only**. `npm install` and `yarn install`
  are blocked by a `preinstall` guard (`only-allow pnpm`).

## Quick start

```bash
git clone https://github.com/Rumeasiyan/ui-design-lab.git my-project
cd my-project
rm -rf .git && git init   # detach from this repo's history
pnpm install
pnpm dev
```

Open the URL Vite prints (default `http://localhost:5173`). You'll land on the three
sample directions.

When you're ready to design your own, delete the samples:

```bash
node scripts/remove-samples.mjs
```

That removes all three sample folders and their registry entries and scaffolds a blank
`v1`. Do it before you start - a sample sitting in the tab bar next to your real work
makes the side-by-side comparison meaningless.

### Keyboard

| Key | Action |
|---|---|
| `1`-`9` | Jump to that direction |
| `←` / `→` | Previous / next direction |
| `↑` / `↓` | Previous / next screen in the active direction |
| `G` | All-directions grid |
| `D` | Light / dark |

Other scripts: `pnpm build`, `pnpm lint`, `pnpm preview`.

For the full screen-by-screen walkthrough of running a session, see
[`USAGE.md`](USAGE.md).

## How it's organized

- **`src/tokens.css`** - single source of design truth. Every color/font/radius/spacing
  a direction uses should be a CSS custom property here, not a hardcoded value in a
  component. Holds two groups: the `--color-*`/`--font-*`/`--radius`/`--gap`/`--motion-*`
  tokens the directions use, and a separate `--chrome-*` set for the harness's own UI.
  Both have a full light-theme override block.
- **`src/directions/`** - one folder per aesthetic direction, each committed to its own
  aesthetic and made up of one or more **screens** (`Page.tsx` as the default, more under
  `screens/<screenId>.tsx`). Register new ones in `src/lib/directions.ts`. Ships with
  three `sample: true` directions - see [`src/directions/README.md`](src/directions/README.md).
- **`src/components/DirectionToggle.tsx`** - tab bar to switch/compare directions live.
  Number keys 1-9 jump directly to a direction, `G` toggles the all-directions grid view.
  A filter input appears once there are more directions than fit comfortably (>6).
- **`src/components/ScreenNav.tsx`** - left sidebar listing the active direction's
  screens (`↑`/`↓` to move); only shows up when a direction has more than one.
- **`src/lib/useKeyboardNav.ts`** - every keyboard shortcut, in one handler, so
  directions and screens can't disagree about the arrow keys.
- **`src/lib/theme.ts`** - light/dark state; flips a `data-theme` attribute that
  `tokens.css` overrides against.
- **`src/components/DeviceFrame.tsx`** - mobile/tablet/desktop preview frames.
- **`src/components/TweakBar.tsx`** - live-adjust tokens (radius, gap, font scale,
  motion) by eye; "Copy CSS" exports the current values back into `tokens.css` shape.

## Inspiration first

**No direction is built until the human has seen references and said what they like.**
Directions written straight from a brief tend to be rejected all at once; a board of about
30 references in 4 to 5 style families, plus one narrowing question, usually reaches a
liked direction in two rounds.

```bash
pnpm inspiration fetch "patient portal" "appointment booking" --per-term 12 \
  --url https://design-system.example.org/
# group the tiles into families in inspiration/families.json, then:
pnpm inspiration board
open inspiration/index.html
```

The script renders Dribbble search pages in local headless Chrome (plain HTTP gets a bot
challenge), downloads thumbnails with curl, optionally screenshots public design-system
pages, and writes a board with tiles numbered A1, A2, B1... linking back to each source.
Needs Google Chrome and curl; set `CHROME_PATH` if Chrome is elsewhere. Walkthrough in
[`USAGE.md`](USAGE.md) section 4.

**The images are other people's copyrighted work.** `inspiration/` is gitignored: look at
them, never commit them, never put one in a direction.

## Adding a new direction

1. `node scripts/new-direction.mjs v2 --label "V2" --sub "Short description"` - creates
   `src/directions/v2/Page.tsx` as the direction's default screen and registers it in
   `src/lib/directions.ts` in one step.
2. Fill in the page, binding every visual value to a `var(--token-name)` from
   `tokens.css` (add new tokens there and to `TweakBar.tsx`'s `FIELDS` list if you need
   new tunable knobs). Size type in `em` - `DeviceFrame` derives the preview's base font
   size from `--font-scale`, and a `rem` would ignore the slider. Check the result in
   both themes.
3. Same intent/guardrails across all directions in a session; each direction fully
   commits to its own aesthetic - never blend two directions in one component. See
   [`PROMPT_TEMPLATE.md`](PROMPT_TEMPLATE.md) for the prompt pattern to drive this.

## Adding a screen to a direction

For a direction with more than one screen (e.g. a flow: Home → Detail → Settings):

```bash
node scripts/new-screen.mjs v2 detail --label "Detail"
```

Creates `src/directions/v2/screens/detail.tsx` and appends it to that direction's
`screens` array in `src/lib/directions.ts`. Once a direction has 2+ screens, the
`ScreenNav` sidebar appears on the left to switch between them, driven by `↑`/`↓`.
Number keys and `←`/`→` still select *directions*, not screens.

## Assets

**A direction is not finished until it carries real imagery.** This is a positive
requirement, not just a ban on placeholders.

A screen built only from type, borders, and flat colour blocks reads as machine-generated -
and that is precisely the judgement a design review exists to make. If you cannot tell
whether a direction is good or just *empty*, it needs assets. Photography, illustration,
texture, pattern, a logotype, a background gradient mesh: whatever the aesthetic calls for.
Generate it.

Three things are equally forbidden: a grey placeholder box, a stock photo, and a design that
quietly avoids imagery so it never has to source any.

```bash
node scripts/imagegen/index.mjs "a topographic contour-line illustration in dark-green ink" src/directions/v1/hero.png --approved
```

`--approved` says you (the human) have said yes to this exact batch. The `codex` provider
runs inside this project and reads its "never generate without asking" rule, so without
the flag it stops to ask for confirmation and saves nothing; the script then fails with an
error naming the flag. An agent passes it only after your recorded yes.

**If an AI agent is doing the work, it must ask you before running this** - for every
provider, not just the paid one. `codex` spends your ChatGPT Plus session, `openai` bills
per image, and `local` occupies your GPU. An agent must also never guess where a local
generation server lives: on a machine it has just cloned onto, it cannot know, so if
`IMAGE_GEN_BASE_URL` is unset the correct move is to ask rather than probe ports or hunt
for a model directory. Both rules are written into
[`AGENTS.md`](AGENTS.md) and [`CLAUDE.md`](CLAUDE.md).

### Providers

Set `IMAGE_GEN_PROVIDER` in `.env` (copy from `.env.example`):

| Provider | Cost | Setup |
|---|---|---|
| `codex` *(default)* | None beyond your existing ChatGPT Plus subscription | `codex` CLI installed and logged in (`codex login`). No API key. |
| `local` | None - runs on your own GPU | Set `IMAGE_GEN_BASE_URL` to your generation server. |
| `openai` | Paid per image | Set `OPENAI_API_KEY`. |

The `local` provider talks to any server exposing `POST /api/generate` →
`GET /api/jobs/{id}` → `GET /api/jobs/{id}/output?index=0`.
[story-panel-studio](https://github.com/Rumeasiyan/story-panel-studio) is one such server -
self-hosted SDXL/FLUX generation on a consumer GPU, so a direction can have as many
revisions of an image as it needs at no marginal cost.

**No host path is committed to this repo.** The local provider is configured entirely from
`.env`, so the location of your server stays yours. Note that such servers typically have
**no authentication** - keep `IMAGE_GEN_BASE_URL` on loopback unless you have put auth in
front of it.

**Verification status:** `codex` and `local` are both **unverified end-to-end** - see open
issues #2 and #13.

## Motion

**A direction must make a deliberate motion decision.** Same logic as imagery: either it
moves, or its stillness is a stated aesthetic choice. What fails review is a direction that
simply never considered motion - a completely inert interface reads as a screenshot and
can't be judged for feel.

Every transition must be built from the motion tokens:

```tsx
style={{
  transitionDuration: 'calc(var(--motion-duration) * 220ms)',
  transitionTimingFunction: 'var(--motion-ease)',
}}
className="hover:-translate-y-[calc(var(--reveal-distance)*0.15*var(--motion-on))]"
```

A hardcoded `transition: 200ms ease` is the motion equivalent of a hardcoded hex - it makes
the TweakBar's Motion group a lie. Check by flipping Motion off: the direction must
actually stop moving.

`--motion-on` defaults to `0` under `prefers-reduced-motion: reduce`, so honouring the OS
setting is automatic for any direction that gates its motion properly. The TweakBar can
still switch it back on - the preference is the default, not a lock.

## Visuals first

**Show, don't tell.** A screen should be understood by looking at it. The hero of every
screen is a visual of the real thing it is about: a map with the places on it, a route
with the item travelling along it, a document or stamp that changes state, a building
filled to its value, two things connecting when they match. Numbers, tables and charts
support that picture; they are never the hero. Moving parts carry the change, built from
the motion tokens. Every visual keeps a text equivalent, and colour is never the only
signal. To check a screen, cover the text with your hand: the state must still be obvious.

## Video generation

```bash
node scripts/videogen/index.mjs "golden-hour clouds built from binary digits" --duration 8 --aspect 16:9
```

**Off by default.** With no `VIDEO_GEN_PROVIDER` set (or set to `manual`), this calls no
API and costs nothing - it prints the composed prompt for you to paste into Google Flow
(Veo, already covered by an AI Pro subscription's Flow credits) by hand.

To turn on paid automation for a specific project, set in `.env` (copy from
`.env.example`):

```bash
VIDEO_GEN_PROVIDER=replicate   # or: openrouter, atlascloud
REPLICATE_API_TOKEN=...
```

**The three paid providers are unverified guesses against real API docs** - see open
issue #3 before trusting their request/response shapes.

## FAQ

### What is UI Design Lab?

UI Design Lab is an open-source template repository for running UI/UX design experiments.
It renders several distinct visual "directions" for the same interface side by side in
one browser window, driven by a shared set of CSS design tokens you can tune live with
sliders. You clone it per design project rather than installing it as a dependency.

### What problem does it solve?

Choosing a visual direction from static mockups is unreliable - you can't feel spacing,
motion, or type scale in a PNG. UI Design Lab renders every candidate as a real,
interactive React screen under identical content and identical device widths, so the
comparison is fair, then lets you tune the winner's tokens by eye and export the result.

### How is this different from Storybook?

Storybook catalogs the *components of one design system*. UI Design Lab compares
*several competing design systems* against the same screen. Storybook answers "what
states does this button have?"; UI Design Lab answers "which of these five aesthetics
should we build?". They solve different stages of the process and can coexist.

### How is it different from a Figma file?

Figma directions are static and disconnected from the code you will ship. Here every
direction is real React + CSS running in a browser, so motion, responsiveness, and
actual token values are all live, and the tuned output is CSS you paste into a project.

### Do I need an AI agent to use it?

No. The harness is plain Vite + React + Tailwind and works fine hand-authored. It is
*optimized* for AI-assisted sessions - `PROMPT_TEMPLATE.md` gives a prompt structure that
reliably produces distinct directions - but nothing requires an agent.

### Do the image and video generation scripts cost money?

Image generation defaults to the `codex` provider, billed against your existing ChatGPT
Plus session with no separate API key. The `local` provider costs nothing per image - it
runs on your own GPU. Only the `openai` provider is paid per image, and it is opt-in.
Video generation is **off by default**
(`VIDEO_GEN_PROVIDER=manual`): it composes a prompt for you to paste into Google Flow by
hand and makes no API call. Paid video providers are strictly opt-in per project.

### Why is it pnpm-only?

To keep one lockfile authoritative. A mixed `package-lock.json` / `pnpm-lock.yaml` repo
produces different dependency trees for different contributors, which in a design harness
shows up as subtle visual differences. An `only-allow pnpm` `preinstall` guard enforces
it.

### Can I use it commercially?

Yes. It is MIT licensed - clone it, modify it, ship client work from it. See
[`LICENSE`](LICENSE).

### Why does it insist on generated images?

Because a UI made of nothing but type and boxes looks AI-generated, and a direction review
that can't distinguish "restrained" from "unfinished" is worthless. Real imagery, texture,
and graphic detail are what make a design read as considered. The harness ships image
generation for exactly this reason, with a zero-marginal-cost local option so there is no
excuse to skip it.

### How do I delete the sample directions?

`node scripts/remove-samples.mjs`. It deletes every direction marked `sample: true` -
folders and registry entries - and scaffolds a blank `v1` if that leaves nothing behind.
Directions you created yourself are never touched. Pass `--dry-run` to see what it would
remove first.

### Does it support light mode?

Yes. `src/tokens.css` defines a full `:root[data-theme="light"]` override for every color
token, and the harness's own UI has a matching `--chrome-*` set, so the tool stays legible
too. Toggle with `D` or the button in the top bar. The toggle deliberately lives in the
harness chrome rather than inside a rendered screen, so it never becomes part of the
design you're evaluating.

### How do I add my own design tokens?

Add the custom property to `src/tokens.css`, then add a matching entry to the `FIELDS`
list in `src/components/TweakBar.tsx`. Adding it to only one of the two breaks either the
tuning UI or the "Copy CSS" export.

## Project status

Functional (builds and lints clean), **pre-1.0**. Several pieces are unverified in a real
browser or against real API docs - run `gh issue list --label unverified`, or see
[`AGENTS.md`](AGENTS.md) for the full picture. Once every `unverified` issue is closed,
this cuts `1.0.0`.

Open work items, bugs, and unverified pieces are tracked as
[GitHub issues](https://github.com/Rumeasiyan/ui-design-lab/issues), not in a doc in this
repo.

## Documentation map

| Doc | What's in it |
|---|---|
| [`USAGE.md`](USAGE.md) | Step-by-step walkthrough of running a design session. |
| [`PROMPT_TEMPLATE.md`](PROMPT_TEMPLATE.md) | The Aesthetic / Reference / Intent / Guardrails prompt pattern, plus a worked example. |
| [`AGENTS.md`](AGENTS.md) | Conventions, hard constraints, and versioning policy - for human and AI contributors alike. |
| [`DECISIONS.md`](DECISIONS.md) | Load-bearing design decisions and the reasoning behind them. |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to set up, what a PR must satisfy. |
| [`CHANGELOG.md`](CHANGELOG.md) | What changed in each version. |
| [`SECURITY.md`](SECURITY.md) | How to report a vulnerability, and this repo's threat surface. |

## Contributing

Contributions are welcome - read [`CONTRIBUTING.md`](CONTRIBUTING.md) first, especially
the hard rules (tokens are the single source of truth; directions never blend; no
placeholder art). Open an issue before starting non-trivial work.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE) © Rumeasiyan
