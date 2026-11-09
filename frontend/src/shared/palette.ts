import { ref, shallowRef } from 'vue'

import type { IconName } from '@/components'

/**
 * The ⌘K palette's state, and the seam its results plug into.
 *
 * A source is a function from what was typed to what matches, registered by
 * whatever owns that data, and handed a signal that aborts once the palette has
 * moved on. The palette asks every source and paints each answer as it arrives,
 * so a source answering from memory is never held back by one asking a server.
 * With none registered it says so rather than inventing results.
 */
export type PaletteItem = {
  id: string
  label: string
  hint?: string
  icon: IconName
  /** The heading it is listed under. Items with none are listed without one. */
  group?: string
  run: () => void
  /**
   * The row's destinations, drawn on its right, the first being what `run`
   * does. Each says where it goes in words a screen reader can announce.
   */
  actions?: PaletteAction[]
}

export type PaletteAction = { label: string; describe: string; run: () => void }

/**
 * What a source answers: its items, optionally a line about them, and
 * optionally a later answer to the same query, for a source whose answer is
 * known to be incomplete. The palette follows it while the query stands.
 */
export type PaletteAnswer =
  | PaletteItem[]
  | { items: PaletteItem[]; status?: string; again?: () => Promise<PaletteAnswer> }

export type PaletteSource = (
  query: string,
  signal: AbortSignal,
) => PaletteAnswer | Promise<PaletteAnswer>

const open = ref(false)
const sources = shallowRef<PaletteSource[]>([])

export function registerPaletteSource(source: PaletteSource): () => void {
  sources.value = [...sources.value, source]
  return () => {
    sources.value = sources.value.filter((known) => known !== source)
  }
}

export function usePalette() {
  return {
    open,
    sources,
    show: () => {
      open.value = true
    },
    hide: () => {
      open.value = false
    },
    toggle: () => {
      open.value = !open.value
    },
  }
}

/** Whether a key press is the palette's shortcut: ⌘K on a Mac, Ctrl+K elsewhere. */
export function isPaletteShortcut(event: KeyboardEvent): boolean {
  return event.key.toLowerCase() === 'k' && (event.metaKey || event.ctrlKey) && !event.altKey
}
