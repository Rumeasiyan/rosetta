import { lazy, type ComponentType, type LazyExoticComponent } from 'react'

export interface Screen {
  id: string
  label: string
  component: LazyExoticComponent<ComponentType>
}

export interface Direction {
  id: string
  key: string // number-key shortcut, e.g. "1" (shortcuts only go up to "9")
  label: string
  sub?: string
  /*
    Marks a direction that ships with the template as an example rather than being part of
    your project. The tab bar renders a SAMPLE badge for these, and
    `node scripts/remove-samples.mjs` deletes every one of them plus its registry entry.
    Never set this on a direction you actually intend to keep.
  */
  sample?: boolean
  screens: Screen[] // first entry is the default screen shown when the direction opens
}

/*
  Add one entry per direction (see PROMPT_TEMPLATE.md). Each direction is its own set of
  screens under src/directions/<id>/ - screens[0] is conventionally Page.tsx, additional
  screens live in src/directions/<id>/screens/<screenId>.tsx. Committed fully to its own
  aesthetic - do not blend directions inside one component. Use scripts/new-direction.mjs
  and scripts/new-screen.mjs instead of hand-editing this file.

  Keyboard: number keys select a direction, Left/Right cycle directions, Up/Down cycle the
  active direction's screens (see src/lib/useKeyboardNav.ts).
*/
export const directions: Direction[] = [
  {
    id: 'stone', key: '1', label: 'Stone bands', sub: 'A3 in ROSETTA colours: stone, olive, brown',
    screens: [
      { id: 'home', label: 'A term', component: lazy(() => import('../directions/stone/Page')) },
      { id: 'list', label: 'Glossary', component: lazy(() => import('../directions/stone/screens/list')) },
      { id: 'memory', label: 'Memory recheck', component: lazy(() => import('../directions/stone/screens/memory')) },
      { id: 'status', label: 'Status', component: lazy(() => import('../directions/stone/screens/status')) },
    ],
  },
]
