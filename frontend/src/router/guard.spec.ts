import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const refresh = vi.fn()
const state: { caller: unknown } = { caller: null }

vi.mock('@/stores/session', () => ({
  useSessionStore: () => ({
    get caller() {
      return state.caller
    },
    refresh,
  }),
}))

const { requireSession, requireAccess } = await import('./index')

const caller = {
  id: 'x',
  username: 'developer',
  roles: ['developer'],
  permissions: [],
  // A deployment with both optional paths configured. The rail hides an entry
  // whose feature this Avior was never given a credential for, so a case about
  // permissions has to say the deployment is not the thing under test.
  features: ['write', 'release'],
}

describe('requireSession', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    refresh.mockReset()
    state.caller = null
  })

  it('lets a public route through without touching the session', async () => {
    await expect(requireSession({ meta: { public: true }, fullPath: '/login' })).resolves.toBe(true)
    expect(refresh).not.toHaveBeenCalled()
  })

  // A route that says nothing is private. Forgetting to think about access
  // control has to fail closed.
  it('redirects to login when a private route has no session', async () => {
    const result = await requireSession({ meta: {}, fullPath: '/reports?range=7d' })

    expect(result).toEqual({ name: 'login', query: { redirect: '/reports?range=7d' } })
    expect(refresh).toHaveBeenCalledOnce()
  })

  it('lets a private route through once the session resolves', async () => {
    refresh.mockImplementation(() => {
      state.caller = caller
      return Promise.resolve()
    })

    await expect(requireSession({ meta: {}, fullPath: '/' })).resolves.toBe(true)
  })

  it('does not refresh again when the caller is already known', async () => {
    state.caller = caller

    await expect(requireSession({ meta: {}, fullPath: '/' })).resolves.toBe(true)
    expect(refresh).not.toHaveBeenCalled()
  })
})

/*
 * requireAccess is for the address bar.
 *
 * The rail already hides what somebody cannot use, so this catches a typed URL,
 * a stale bookmark and a link somebody was sent — the ways of arriving at a
 * page that would refuse you without ever having been offered it.
 */
describe('requireAccess', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    state.caller = null
  })

  it('lets a caller through to a page their permissions open', () => {
    state.caller = { ...caller, permissions: ['query.inspect'] }

    expect(requireAccess({ name: 'query-console', meta: {} })).toBe(true)
  })

  // The destination is the first entry of their own rail rather than a fixed
  // page: a release approver holds no query permission, and the query console
  // would refuse them just as thoroughly as the page they asked for.
  it('sends an approver to the page they would have clicked', () => {
    state.caller = {
      ...caller,
      roles: ['release_approver'],
      permissions: ['release.approve'],
    }

    expect(requireAccess({ name: 'admin-accounts', meta: {} })).toEqual({
      name: 'release-pending',
    })
  })

  // FEAT-071: the owner opened the observatory to every signed-in operator on 2026-09-27,
  // and it is never where somebody lands, so an approver still starts on their own page.
  it('opens the observatory to anybody signed in without making it their landing page', () => {
    state.caller = { ...caller, roles: ['release_approver'], permissions: ['release.approve'] }
    expect(requireAccess({ name: 'observatory', meta: {} })).toBe(true)
    expect(requireAccess({ name: 'admin-accounts', meta: {} })).toEqual({ name: 'release-pending' })
  })

  it('sends a developer asking for the admin pages back to the console', () => {
    state.caller = { ...caller, permissions: ['query.inspect', 'audit.read.own'] }

    expect(requireAccess({ name: 'admin-databases', meta: {} })).toEqual({ name: 'query-console' })
  })

  /*
   * FEAT-007 provisions an account on first sign-in and a deployment may grant
   * it nothing, so somebody holding no role at all is an ordinary first day.
   *
   * They land on the console, which is where a rail with nothing in it sends
   * everybody.
   *
   * This used to be the release queue, because those pages asked only for a
   * session and so were the one thing anybody could reach. The first person to
   * sign in to a new deployment therefore arrived at the approval queue for
   * production schema changes, which is a strange first thing to be shown and
   * was an accident of which page happened to need no permission.
   */
  it('lands somebody who holds no role at all', () => {
    state.caller = { ...caller, roles: [], permissions: [], features: [] }

    const landing = requireAccess({ name: 'query-console', meta: {} })
    expect(landing).not.toBe(true)
    expect(landing).toEqual({ name: 'query-console' })
  })

  // A page the rail does not name is reached by following a link — one release,
  // one table's structure — and is guarded by its own endpoints.
  it('leaves a page the rail does not name alone', () => {
    state.caller = { ...caller, permissions: [] }

    expect(requireAccess({ name: 'release-detail', meta: {} })).toBe(true)
  })

  it('leaves the login page alone', () => {
    expect(requireAccess({ name: 'login', meta: { public: true } })).toBe(true)
  })
})
