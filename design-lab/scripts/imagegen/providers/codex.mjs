/*
  Default provider. Wraps `codex exec`'s built-in image tool — billed against your existing
  ChatGPT Plus session, so no separate API key and no per-image charge. See DECISIONS.md for
  why this rather than a paid image API.

  Requires: the `codex` CLI installed and logged in (`codex login`).

  codex runs inside the project, so it reads the project's AGENTS.md, which says never to
  generate without asking the human. Asked cold, codex answers "please confirm" and saves
  nothing. With `approved` (the --approved flag), the prompt states that the human
  approved this exact batch, so codex goes ahead.
*/
import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'

export async function generate({ prompt, output, width, height, approved = false }) {
  const lines = [
    `generate an image: ${prompt}`,
    `Dimensions: ${width}x${height}.`,
    `Save the final result to exactly this path: ${output}`,
  ]
  if (approved) {
    lines.push(
      'The human has already approved generating this exact image (this prompt, this path, this provider). Generate it now without asking for confirmation.',
    )
  }
  const fullPrompt = lines.join('\n')

  execFileSync('codex', ['exec', fullPrompt, '--sandbox', 'workspace-write', '--skip-git-repo-check'], {
    stdio: 'inherit',
  })

  if (!existsSync(output)) {
    if (!approved) {
      throw new Error(
        `codex saved nothing at ${output}. It most likely stopped to ask for confirmation, because ` +
          `the project's AGENTS.md says never generate without asking. If the human has approved this ` +
          `exact batch, run again with --approved; otherwise ask them first.`,
      )
    }
    throw new Error(`codex reported success but ${output} does not exist; check the codex output above.`)
  }

  return { path: output }
}
