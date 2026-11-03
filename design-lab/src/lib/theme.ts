import { useCallback, useEffect, useState } from 'react'

/*
  Light/dark is a harness-level preview control, not part of any direction's design — the
  toggle lives in the top bar, never inside a rendered screen. It flips a `data-theme`
  attribute on <html>; `src/tokens.css` does the rest via its
  `:root[data-theme='light']` override block.
*/

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'ui-design-lab:theme'

function readStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark') return stored
  } catch {
    // Private-mode browsers throw on storage access — fall through to the default.
  }
  return 'dark'
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readStoredTheme)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      localStorage.setItem(STORAGE_KEY, theme)
    } catch {
      // Not being able to remember the choice is not worth breaking the app over.
    }
  }, [theme])

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return { theme, setTheme, toggleTheme }
}
