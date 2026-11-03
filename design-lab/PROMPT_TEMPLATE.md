# Prompt template - starting a design session

Copy the template below, fill in the four fields, hand it to an AI coding agent working
in this repo (or use it as your own brief). But first, step 0.

## Step 0: Inspiration (before writing any direction)

Do not write directions from the brief alone. Directions built straight from a written
brief tend to be rejected all at once; people recognise what they like far faster than
they can describe it. Show references first:

1. **Search terms from the product's domain and users**, not from style words: "patient
   portal", "warehouse inventory", "government website", not "clean modern minimal".
   Optionally a few public design-system pages that suit the audience.
2. **Build the board:** `pnpm inspiration fetch "term one" "term two" --per-term 12
   [--url https://...]`, then group about 30 tiles into **4 to 5 style families** in
   `inspiration/families.json` and run `pnpm inspiration board`. Tiles are numbered by
   family: A1, A2, B1, B3...
3. **Ask which families** the human likes (several may be chosen).
4. **Ask which tiles** within a chosen family when the family mixes styles.
5. **Build 2 to 3 directions from the picks.** Each direction's `sub` label names the
   tiles it came from, e.g. `--sub "Calm editorial (A1, A4)"`, and its `Reference:` line
   below points at those tiles.

Full walkthrough: `USAGE.md` section 4. The board's images are other people's work:
match their feel, never copy them into a direction, and never commit `inspiration/`.

An agent's first message of a session therefore looks like this:

```text
Before I design anything, here are 32 references grouped into 5 families:
open inspiration/index.html. Which families do you like (pick any)?
A Calm editorial, B Dense operational, C Friendly illustrated, D Civic and formal,
E Dark data. If one you pick mixes styles, I'll ask which tiles next.
```

## The pattern

```text
Aesthetic: [family] - [5-8 vocabulary terms]
Reference: [screenshot or image path] - match feel, not content.
Intent: [what this should feel like and why]
Guardrails: always [constants]. Never [bans].
```

- **Aesthetic** - name the visual family (e.g. "print-tech x data", "brutalist-editorial
  B&W") plus 5-8 concrete vocabulary terms, not vibes. "Clean and modern" is not a
  vocabulary term; "topographic line illustration, mono data callouts, film-strip ticks"
  is.
- **Reference**: the board tiles the human picked in step 0 (e.g. "A1, A4"), or another
  real screenshot or image, with an explicit instruction to match *feel*, not content. Without this line an agent will often copy the reference's literal
  subject matter instead of its visual language.
- **Intent** - the emotional/functional goal, tied to a real outcome ("a founder should
  think X within 3 seconds"), not just an adjective.
- **Guardrails** - explicit allow/ban lists. Vague taste doesn't transfer; a concrete
  "never gradient, never Inter-only" does. Always include the visuals-first rule from
  `AGENTS.md`: the hero of every screen is a picture of the real thing, not a counter or chart.

## Multi-direction session template

For generating several directions at once (the harness's core use case - see
`src/lib/directions.ts` and the `DirectionToggle` tab bar), wrap the pattern in this
shared-base structure:

```text
Build [what you're building].
Conversion/functional goal: [the one thing this page/screen must accomplish].

Intent: [shared intent across ALL directions - what it should feel like and why].

Guardrails - always: [constants shared across all directions].
Never: [bans shared across all directions].

Create N versions, one per direction below. Same intent and guardrails for all of them.
Do NOT blend directions - each version commits fully to its own aesthetic.

--- DIRECTION 1 (v1) - [Direction Name] ---
Aesthetic: [family] - [5-8 vocabulary terms]
Reference: [path/description] - read it before designing; match feel, not content.
Placement: [where the key visual/content sits and how it composes with the layout]

--- DIRECTION 2 (v2) - [Direction Name] ---
...
```

## Worked example (Kestrel, condensed - full transcript in the notes repo)

```text
Build a landing page for "Kestrel" - an AI analytics platform for small startups.
Conversion goal: book a demo. Primary CTA on every version is "Book a demo"; it must
appear in the hero and repeat at the end of the page.

Intent: a small team's unfair advantage. Should feel like serious, crafted intelligence
 -  calm and confident - not loud SaaS hype. A founder should think "these people actually
understand data" within 3 seconds.

Guardrails - always: one monumental image anchors the page; imagery is processed, never
raw (halftone, dither, grain, ASCII, linework); technical marginalia (coordinates, IDs,
ruler ticks, timestamps); type at extremes - monumental display or tiny mono labels,
little middle; near-monochrome ground with a single warm accent.
Never: purple gradients, glossy 3D SaaS blobs, untextured stock photography,
rounded-everything friendliness, icon-grid feature rows, Inter/system-font-only
typography, evenly-distributed colorful palettes.

Create 5 versions, one per direction below. Same intent and guardrails for all five.
Do NOT blend directions - each version commits fully to its own aesthetic.

--- DIRECTION 1 (v1) - Print-Tech Paper ---
Aesthetic: print-tech x data - pale sage ground, topographic line illustration, mono
data callouts, transaction-ID chips, film-strip ticks, grotesk display.

--- DIRECTION 2 (v2) - Data-as-Texture ---
Aesthetic: cinematic data-texture - clouds rendered from amber binary characters, dark
teal sky, data-as-material, golden accent CTAs, mono labels.

--- DIRECTION 3 (v3) - Vast Quiet Cinematic ---
Aesthetic: editorial minimalism x cinematic - vast B&W mountain photography, mist
atmosphere, tiny centered sans, extreme whitespace, quiet CTA.

--- DIRECTION 4 (v4) - Dither Mono ---
Aesthetic: brutalist-editorial B&W - heavy bitmap dither, stark studio dark, giant
cropped footer wordmark, clean sans body, high contrast.

--- DIRECTION 5 (v5) - Classical Remix ---
Aesthetic: classical x white editorial - grainy classical figure illustration, serif
italic emphasis word, white ground with thin orbit lines, blue pill CTA, trust logo row.
```

## Turning a filled-in prompt into this harness's directions

1. Run step 0 and get the human's picks; build 2 to 3 directions from them.
2. Fill in the template above with your project's specifics.
3. For each direction block, run:
   ```bash
   node scripts/new-direction.mjs v1 --label "V1" --sub "Print-Tech Paper (A1, A4)"
   ```
   (creates `src/directions/v1/Page.tsx` and registers it in `src/lib/directions.ts` -
   see `scripts/new-direction.mjs --help`.)
4. Fill in each `Page.tsx` per its Aesthetic/Placement, binding every visual value to a
   `var(--token-name)` from `src/tokens.css` (add new tokens if a direction needs a
   value not yet tunable; see `USAGE.md` section 5).
5. For hero imagery, use `scripts/imagegen/index.mjs`: **ask the human before running
   it**, whichever provider is configured; it spends a subscription, an API balance, or
   GPU time. After the human's yes to that batch, pass `--approved` (without it the
   `codex` provider stops to ask and saves nothing). If `IMAGE_GEN_BASE_URL` is needed and unset, ask for the address rather than
   probing for it. Also state the direction's motion decision explicitly, built from
   `--motion-duration` / `--motion-ease` / `--reveal-distance` and gated by `--motion-on` (or the manual/video-API paths in
   `scripts/videogen/`) per direction's described visual.
