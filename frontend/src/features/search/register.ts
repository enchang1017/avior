import type { RouteLocationRaw } from 'vue-router'

import { navigation, visibleTo } from '@/layouts/navigation'
import { router } from '@/router'
import { registerPaletteSource } from '@/shared/palette'
import { useSessionStore } from '@/stores/session'
import { searchPages, searchServer } from './engine'
import { pagesOf } from './pages'

/*
 * FEAT-066 plugged into the shell's palette. Loaded as a chunk of its own once
 * the shell has mounted, so the shell carries nothing of search but the line
 * that loads this; importing it twice registers once.
 */
const go = (to: RouteLocationRaw) => void router.push(to)
const session = useSessionStore()

/** The pages the rail offers this caller, which are the pages an action may lead to. */
const mayOpen = (route: string) => {
  const caller = session.caller
  return (
    caller !== null && pagesOf(visibleTo(navigation, caller)).some((page) => page.route === route)
  )
}

registerPaletteSource((query) => searchPages(query, session.caller, go))
registerPaletteSource((query, signal) => searchServer(query, signal, { go, mayOpen }))
