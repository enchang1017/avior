import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const refresh = vi.fn()
const state = { caller: null as unknown }

vi.mock('@/stores/session', () => ({
  useSessionStore: () => ({
    get caller() {
      return state.caller
    },
    refresh,
  }),
}))

describe('router guard', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    refresh.mockReset()
    state.caller = null
  })

  it('marks login public and everything else private', async () => {
    const { router } = await import('./index')
    const login = router.getRoutes().find((route) => route.name === 'login')
    const console = router.getRoutes().find((route) => route.name === 'query-console')

    expect(login?.meta['public']).toBe(true)
    expect(console?.meta['public']).toBeUndefined()
  })

  it('routes the unknown path to the console rather than 404ing', async () => {
    const { router } = await import('./index')
    const catchAll = router.getRoutes().find((route) => route.path.includes('pathMatch'))

    expect(catchAll?.redirect).toBe('/')
  })
})

/*
 * A route name is a string. TypeScript does not check it and the router only
 * complains at runtime, so a menu entry or a link pointing at a route that was
 * renamed or deleted passes every other gate and breaks when somebody clicks
 * it. Both halves of the release rename did exactly that.
 */
describe('route names referenced elsewhere', () => {
  it('gives every navigation entry a route that exists', async () => {
    const { router } = await import('./index')
    const { navigation, isGroup } = await import('@/layouts/navigation')

    const known = new Set(router.getRoutes().map((route) => route.name))
    const referenced = navigation.flatMap((entry) =>
      isGroup(entry) ? entry.children.map((child) => child.route) : [entry.route],
    )

    for (const name of referenced) {
      if (name === undefined) continue
      expect(known.has(name), `the menu points at ${name}, which no route defines`).toBe(true)
    }
  })

  // ReleaseDetailView builds its back link as `release-${scope}`, which no
  // amount of type checking can follow.
  it('gives every release scope a list route', async () => {
    const { router } = await import('./index')
    const { scopeStates } = await import('@/features/release/useReleaseQueue')

    const known = new Set(router.getRoutes().map((route) => route.name))
    for (const scope of Object.keys(scopeStates)) {
      expect(known.has(`release-${scope}`), `no route for the ${scope} list`).toBe(true)
    }
  })
})
