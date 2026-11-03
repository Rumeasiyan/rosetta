@AGENTS.md

Claude-specific: if you delegate to a subagent (Task tool), it does not inherit this
file — restate the relevant Constraints from `AGENTS.md` in the subagent prompt. The ones
that get violated by default instinct, in order of how much they cost to get wrong:

1. **Never start image or video generation without asking the human first** — any
   provider, including `local`. A subagent told "this direction needs a hero image" will
   otherwise just generate one and spend the human's subscription or GPU time.
2. **Never guess where a local generation server lives.** If `IMAGE_GEN_BASE_URL` is
   unset, ask. Do not probe ports or hunt for a model directory on disk.
3. **`tokens.css` is the single source of design truth** — no hardcoded visual values,
   motion included.
4. **`VIDEO_GEN_PROVIDER` defaults to `manual`** and stays there unless the human says
   otherwise.
5. **No direction before an inspiration board.** Fetch references with
   `scripts/inspiration/index.mjs`, group them into 4 to 5 families, ask the human which
   families and tiles they like, and only then build directions from the picks. Never
   commit `inspiration/` (other people's copyrighted images).
