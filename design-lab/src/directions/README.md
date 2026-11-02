# Directions

One folder per aesthetic direction. `Page.tsx` is the direction's default screen;
additional screens live in `screens/<screenId>.tsx`. Register everything in
`src/lib/directions.ts`.

## Making your own

```bash
node scripts/new-direction.mjs v2 --label "V2" --sub "Short description"
node scripts/new-screen.mjs v2 detail --label "Detail"
```

Rules that apply to every direction (see [`../../AGENTS.md`](../../AGENTS.md)):

- Every color/font/radius/spacing value is a custom property from `src/tokens.css`. Never
  hardcode a visual value in a component — the TweakBar and its "Copy CSS" export only
  work if this holds.
- A direction never references a `--chrome-*` token. Those belong to the harness UI.
- Each direction fully commits to its own aesthetic. Never blend two in one component.
- **Real imagery is required, not optional.** No placeholders, no stock art, and no
  quietly avoiding images so none have to be sourced — a screen of pure type and boxes
  reads as machine-generated. Generate with `scripts/imagegen/index.mjs`. If an agent is
  doing the work, it must ask you before generating anything, whichever provider is set.
- **Make a deliberate motion decision.** Either the direction moves — every transition
  built from `--motion-duration`, `--motion-ease` and `--reveal-distance`, gated by
  `--motion-on` — or motion is deliberately absent and that is the choice. Never hardcode
  a duration or easing curve; it makes the TweakBar's Motion group a lie. Flip Motion off
  and confirm the direction actually stops.
- Size type in `em`, not `rem` or `px`, so the TweakBar's Font Scale slider moves it —
  `DeviceFrame` sets the preview's base font size from `--font-scale`.
- Both themes have to work. Toggle light/dark (the `D` key) before calling a direction
  done.
