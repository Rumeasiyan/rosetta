# Usage

How to actually run a design session with this template, screen by screen. For repo
structure/conventions see `README.md`; for contributor rules see `AGENTS.md`.

## 1. Clone for a new project

This repo is meant to be cloned per project, not built on top of directly.

```bash
git clone https://github.com/Rumeasiyan/ui-design-lab.git my-project
cd my-project
rm -rf .git && git init   # detach from this repo's history
pnpm install
pnpm dev
```

Open the URL Vite prints (default `http://localhost:5173`).

## 2. What you're looking at

The app (`src/App.tsx`) has two top bars and three columns:

```
┌──────────────────────────────────────────────────────┐
│ DirectionToggle    Editorial · Brutalist · Soft      │
├──────────────────────────────────────────────────────┤
│ active direction              [◑ dark] [M T Desktop] │
├──────────┬──────────────────────────────┬────────────┤
│ ScreenNav│        DeviceFrame           │  TweakBar  │
│  Home    │      (the direction)         │  sliders   │
│  Article │                              │            │
│  Contents│                              │            │
└──────────┴──────────────────────────────┴────────────┘
```

- **Top tab bar** (`DirectionToggle`) - one tab per registered direction. Click a tab,
  or press its number key (`1`-`9`), to view that direction full-screen. `←`/`→` step
  through directions in order. Press `G` (or click "All N") to switch to a grid showing
  every direction at once, side-by-side. Once there are more than 6 directions, a filter
  input appears - type to narrow the tabs shown by label/subtitle (shortcuts still work
  on the full, unfiltered set). Directions marked `sample: true` carry a red **SAMPLE**
  badge: those are the template's examples, not your work (see section 3).
- **Control bar** - names the active direction, and holds the light/dark toggle (`D`)
  and the mobile/tablet/desktop switch. Both are *harness* controls: the theme toggle
  lives up here, never inside a rendered screen, so it never becomes part of the design
  you're judging.
- **Left sidebar** (`ScreenNav`) - the active direction's screens, listed vertically.
  Click one, or use `↑`/`↓`. It only appears when a direction has more than one screen;
  switching direction always resets to that direction's first screen.
- **Center pane** (`DeviceFrame`) - renders the active screen inside a frame at mobile
  (390px), tablet (834px), or desktop (100%) width.
- **Right sidebar** (`TweakBar`) - a control for every tunable custom property in
  `src/tokens.css`, in five collapsible groups: **Color** (eight colour pickers),
  **Type** (display/body/mono face, scale, weights, tracking, leading, measure),
  **Shape** (radius, border width, gap, padding), **Depth** (shadow strength and
  offset), and **Motion** (on/off, duration, easing, reveal distance). These edit the
  live `:root` styles in the browser, so changes apply instantly to whichever direction
  is showing - nothing is saved until you use "Copy CSS".

### Keyboard

| Key | Does |
|---|---|
| `1`-`9` | Jump to that direction |
| `←` / `→` | Previous / next direction (wraps) |
| `↑` / `↓` | Previous / next screen in the active direction (wraps) |
| `G` | Toggle the all-directions grid |
| `D` | Toggle light / dark |

Shortcuts are suspended while a text field is focused, which is also why nudging a
TweakBar slider with the arrow keys doesn't change direction underneath you.

Grid view has no TweakBar/device-frame controls - it's for comparing directions at a
glance, not tuning.

## 3. Delete the samples first

The template ships with three example directions - Editorial, Brutalist, Soft - three
screens each, so you can see what a finished direction looks like before writing one.
They are not your project. Delete all three:

```bash
node scripts/remove-samples.mjs
```

That removes their folders and registry entries and scaffolds a blank `v1` in their
place. Do it before you start designing: a sample sitting in the tab bar next to your
real work makes the side-by-side comparison meaningless.

## 4. Inspiration first (before any direction)

Do not build a direction until you have seen real references and said what you like. A
written brief is a poor predictor of taste: directions built straight from one tend to be
rejected wholesale, while a board of about 30 references, followed by one narrowing
question, usually reaches a liked direction in two rounds.

1. **Pick search terms from the product, not from styles.** Its domain and its users:
   for a clinic booking app, "clinic booking app", "patient portal", "appointment
   calendar"; for an internal tool, "admin dashboard", "inventory management". Two to
   four terms is enough. Optionally add a few public design-system pages that suit the
   audience.
2. **Fetch the references and build the board:**
   ```bash
   pnpm inspiration fetch "clinic booking app" "patient portal" --per-term 12 \
     --url https://design-system.example.org/
   ```
   This writes `inspiration/` (gitignored): `tiles.json` (every tile, with the shot's
   title and alt text), `img/`, `families.json`, `labels.json` (board label to tile id)
   and `index.html`. Needs Google Chrome and curl; set `CHROME_PATH` if Chrome is not at
   the default macOS location.
3. **Group into 4 to 5 style families.** Look at the images (and the titles in
   `tiles.json`), then edit `inspiration/families.json`:
   ```json
   { "title": "Clinic booking: inspiration",
     "families": [
       { "name": "Calm editorial", "note": "serif headings, lots of white", "tiles": ["25586920", "site1"] },
       { "name": "Dense operational", "note": "dark, tables, side rail", "tiles": ["24946804"] }
     ] }
   ```
   Then rebuild: `pnpm inspiration board`. Families are lettered A, B, C... in the order
   listed and tiles numbered within them (A1, A2, B1...); anything not listed lands in a
   final "Unsorted" group.
4. **Show the board and ask which families** the human likes (more than one is fine).
   Open `inspiration/index.html` in a browser, or send a screenshot of it.
5. **If a chosen family mixes styles, ask which tiles** within it ("B2, B5, not B3").
   Fetch a second, narrower round if nothing in the family fits.
6. **Build 2 to 3 directions from the picks**, and name the source tiles in each
   direction's `sub` label, e.g. `--sub "Calm editorial (A1, A4)"`, so anyone reviewing
   the tab bar can trace each direction back to what was liked.

**The images are other people's copyrighted work.** They are for looking at during the
session. Never commit `inspiration/`, never copy a reference image into a direction, and
never trace one: match feel, not content (see `PROMPT_TEMPLATE.md`).

## 5. Prompting for a direction

Once the human has picked from the inspiration board (section 4), start from
**[`PROMPT_TEMPLATE.md`](PROMPT_TEMPLATE.md)**: the actual fill-in-the-blanks
prompt pattern (Aesthetic/Reference/Intent/Guardrails) plus a full worked example, not
just the rules below. It also covers turning a filled-in prompt into registered
directions via `scripts/new-direction.mjs`.

Each direction is a self-contained page. When prompting an AI agent (or building by
hand) to create one, the two rules that make the harness work are:

1. **Every visual value binds to a `var(--token-name)` from `src/tokens.css`.** Don't
   hardcode a hex color, a border-radius, a font - add a CSS custom property to
   `tokens.css` and reference it. If the TweakBar or "Copy CSS" needs to touch a value
   that isn't a token yet, add it to `tokens.css` **and** to `TweakBar.tsx`'s `FIELDS`
   array - both, or the slider and the export drift out of sync silently.
2. **Each direction commits fully to one aesthetic.** Don't blend two visual languages
   in a single `Page.tsx` - the whole point of side-by-side comparison is that each tab
   is a distinct, coherent bet.
3. **Never use placeholder images, icons, or stock art.** If the design calls for an
   image, generate a real one first (see "Generating supporting assets" below) - a gray
   box or stock photo can't be judged for feel, which is the whole point of reviewing a
   direction.
4. **Size type in `em`, not `rem` or `px`.** `DeviceFrame` sets the preview's base font
   size from `--font-scale`; a `rem` resolves against the document root and would ignore
   the Font Scale slider entirely.
5. **Check both themes.** Press `D` before calling a direction done - every color comes
   from a token, and every token has a light-theme value in `src/tokens.css`.

## 6. Adding a new direction

```bash
node scripts/new-direction.mjs v2 --label "V2" --sub "Short description"
```

Creates `src/directions/v2/Page.tsx` (registered as that direction's default "home"
screen) and registers the direction in `src/lib/directions.ts` in one step - it appears
in the tab bar and grid view automatically, no manual wiring needed. Number-key
shortcut defaults to the next unused digit (1-9); pass `--key` to override.

Then fill in `Page.tsx` per your prompt's Aesthetic/Placement (see
[`PROMPT_TEMPLATE.md`](PROMPT_TEMPLATE.md)), binding every visual value to a
`var(--token-name)` from `tokens.css`.

## 7. Adding a screen to a direction

If a direction is more than one view (e.g. a flow: Home → Detail → Settings), add
screens beyond the default one:

```bash
node scripts/new-screen.mjs v2 detail --label "Detail"
```

Creates `src/directions/v2/screens/detail.tsx` and appends it to `v2`'s `screens` array
in `src/lib/directions.ts`. As soon as a direction has 2+ screens, the `ScreenNav`
sidebar appears on the left to switch between them - `↑`/`↓` move through the list (see
section 2). There's no routing/URL per screen - it's local component state in
`App.tsx`, reset to the first screen whenever you switch directions.

## 8. Tuning by eye

With a direction open (not grid view):

1. Drag TweakBar sliders / flip toggles - the page updates live. Check the result in
   both themes (`D`); a direction isn't done until it holds up in light and dark.
2. When it looks right, click **Copy CSS**. This copies *both* blocks - the shared
   `:root { ... }` and the `:root[data-theme='light'] { ... }` override - with the
   current value of every tunable token.
3. Paste them over the equivalent blocks in `src/tokens.css` to make the tuning permanent
   (the TweakBar only edits the live DOM - a page refresh reverts to whatever
   `tokens.css` says).
4. **Reset** discards tuning in **both** themes and returns to `tokens.css`'s values.

**Colours are tuned per theme.** The panel writes inline styles on `<html>`, which beat
both `:root` and the light block, so a colour picked in light mode would otherwise leak
into dark. Colour and shadow-strength edits are therefore remembered separately per theme
and re-applied when you press `D`; everything else (type, shape, motion) is shared. The
theme you are currently editing is shown at the top of the panel.

## 9. Generating supporting assets

Image and video generation are invoked from the CLI, not the browser UI - see
`README.md`'s "Image generation" / "Video generation" sections for commands and current
verification status. **A direction is not done until it carries real imagery** - not merely "no placeholders",
but no quietly avoiding images either. Generate them with `scripts/imagegen/index.mjs`,
adding `--approved` once you have said yes to the batch (the `codex` provider otherwise
stops to ask and saves nothing).
Pick a provider in `.env`: `codex` (default, no API key), `local` (your own generation
server, no per-image cost), or `openai` (paid). Video generation defaults
to `manual` mode (no API call) and stays optional/opt-in per the constraint in
`AGENTS.md`.

## 10. Wrapping up a session

There's no "export the whole app" step - a direction's `Page.tsx` plus the relevant
slice of `tokens.css` *is* the deliverable. Once a direction is settled:

- Make sure its final tuning has been copied into `tokens.css` (section 8).
- Hand off / reuse `Page.tsx` and the token values directly in the target project.

## Keyboard reference

| Key | Action |
|---|---|
| `1`-`9` | Jump to the direction registered with that key |
| `←` / `→` | Previous / next direction (wraps) |
| `↑` / `↓` | Previous / next screen in the active direction (wraps) |
| `G` | Toggle all-directions grid view |
| `D` | Toggle light / dark theme |

Shortcuts are disabled while focus is inside a text input/textarea. That guard is what
keeps arrow-key nudges on a TweakBar slider (`<input type="range">`) from also changing
direction. All of it lives in one handler, `src/lib/useKeyboardNav.ts`.
