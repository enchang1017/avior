import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { useSessionStore } = await import('./session')

const caller = {
  id: '00000000-0000-0000-0000-000000000001',
  username: 'developer',
  roles: ['developer' as const],
  permissions: ['query.execute'],
}

describe('session store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    get.mockReset()
    post.mockReset()
  })

  it('loads the caller on refresh', async () => {
    get.mockResolvedValue({ data: caller, response: { status: 200 } })
    const session = useSessionStore()

    await session.refresh()

    expect(session.caller).toEqual(caller)
    expect(session.failure).toBeNull()
  })

  // Being signed out is the normal state on first load, not an error to show.
  it('treats a 401 on refresh as simply not signed in', async () => {
    get.mockResolvedValue({ data: undefined, error: null, response: { status: 401 } })
    const session = useSessionStore()

    await session.refresh()

    expect(session.caller).toBeNull()
    expect(session.failure).toBeNull()
  })

  it('reports a genuine failure on refresh', async () => {
    get.mockResolvedValue({
      data: undefined,
      error: { code: 'internal', message: 'boom' },
      response: { status: 500 },
    })
    const session = useSessionStore()

    await session.refresh()

    expect(session.caller).toBeNull()
    expect(session.failure).toEqual({ kind: 'error', code: 'internal', message: 'boom' })
  })

  it('stores the caller after a successful sign-in', async () => {
    post.mockResolvedValue({
      data: { user: caller, expiresAt: '2026-09-01T12:00:00Z' },
      response: { status: 200 },
    })
    const session = useSessionStore()

    await expect(session.signIn('developer', 'avior')).resolves.toBe(true)
    expect(session.caller).toEqual(caller)
  })

  it('reports a rejected sign-in without storing a caller', async () => {
    post.mockResolvedValue({
      data: undefined,
      error: { code: 'auth.invalid_credentials', message: 'Those credentials were not accepted.' },
      response: { status: 401 },
    })
    const session = useSessionStore()

    await expect(session.signIn('developer', 'wrong')).resolves.toBe(false)
    expect(session.caller).toBeNull()
    expect(session.failure).toEqual({ kind: 'unauthorised' })
  })

  it('drops the caller on sign-out', async () => {
    get.mockResolvedValue({ data: caller, response: { status: 200 } })
    post.mockResolvedValue({ data: undefined, response: { status: 204 } })
    const session = useSessionStore()
    await session.refresh()

    await session.signOut()

    expect(session.caller).toBeNull()
  })
})
