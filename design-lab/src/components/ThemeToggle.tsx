import type { Theme } from '../lib/theme'

/*
  Light/dark is a preview control belonging to the harness, so it lives in the top bar
  beside the device switch — never inside a rendered screen, where it would become part of
  the direction being judged.
*/
export function ThemeToggle({ theme, onToggle }: { theme: Theme; onToggle: () => void }) {
  return (
    <button
      onClick={onToggle}
      title="Toggle light/dark (D)"
      aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
      className="flex items-center gap-1.5 rounded-md border border-[var(--chrome-border)] bg-[var(--chrome-bg-alt)] px-2 py-1 text-xs uppercase tracking-wide text-[var(--chrome-muted)] hover:text-[var(--chrome-fg)]"
    >
      <span aria-hidden>{theme === 'dark' ? '◑' : '◐'}</span>
      <span>{theme}</span>
    </button>
  )
}
