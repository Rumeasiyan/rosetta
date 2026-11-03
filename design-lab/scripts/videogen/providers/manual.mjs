/*
  Default provider. Does not call any API or spend any money — composes the full prompt and
  hands it back so you can paste it into Google Flow (Veo, already covered by your AI Pro
  subscription's Flow credits) or any other tool by hand.
  See DECISIONS.md for why this is the default.
*/
export async function generate({ prompt, durationSeconds, aspectRatio }) {
  const composed = [
    prompt,
    durationSeconds ? `Duration: ${durationSeconds}s` : null,
    aspectRatio ? `Aspect ratio: ${aspectRatio}` : null,
  ]
    .filter(Boolean)
    .join('\n')

  return {
    mode: 'manual',
    prompt: composed,
    message:
      'VIDEO_GEN_PROVIDER is unset or "manual" — no API called, no cost incurred. ' +
      'Paste the prompt below into Google Flow (or your tool of choice) by hand:',
  }
}
