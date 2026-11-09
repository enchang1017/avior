import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
const del = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post, DELETE: del } }
})

const { useTokens } = await import('./useTokens')

function token(overrides: Record<string, unknown> = {}) {
  return {
    id: '00000000-0000-0000-0000-000000000010',
    name: 'laptop agent',
    createdAt: '2026-09-22T00:00:00Z',
    expiresAt: '2026-09-29T00:00:00Z',
    lastUsedAt: null,
    revokedAt: null,
    ...overrides,
  }
}

function ok(body: unknown, status = 200) {
  return { data: body, error: undefined, response: { status, ok: true } }
}

function refusal(status: number, body: unknown) {
  return { data: undefined, error: body, response: { status, ok: false } }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  del.mockReset()
})

/*
 * The one rule of the page is that the value appears once. The composable
 * holds the minted token apart from the list for that reason, and these cases
 * are about that separation: the list never gains a value, dismissing is
 * final, and a refusal on mint leaves nothing behind to show.
 */
describe('minting', () => {
  it('holds the value apart from the list, and reloads the list', async () => {
    post.mockResolvedValue(ok({ ...token(), token: 'avior_secret' }, 201))
    get.mockResolvedValue(ok([token()]))

    const store = useTokens()
    const made = await store.mint('laptop agent', 7)

    expect(made).toBe(true)
    expect(store.minted.value?.token).toBe('avior_secret')
    expect(post).toHaveBeenCalledWith('/tokens', {
      body: { name: 'laptop agent', expiresInDays: 7 },
    })
    expect(store.tokens.value).toHaveLength(1)
    expect(JSON.stringify(store.tokens.value)).not.toContain('avior_secret')
  })

  it('reports a refusal and mints nothing', async () => {
    post.mockResolvedValue(refusal(422, { code: 'token.name_blank', message: 'Give it a name.' }))

    const store = useTokens()
    const made = await store.mint('', 7)

    expect(made).toBe(false)
    expect(store.minted.value).toBeNull()
    expect(store.failure.value?.kind).toBe('error')
  })

  it('dismissing the value is final', async () => {
    post.mockResolvedValue(ok({ ...token(), token: 'avior_secret' }, 201))
    get.mockResolvedValue(ok([token()]))

    const store = useTokens()
    await store.mint('x', 7)
    store.dismissMinted()

    expect(store.minted.value).toBeNull()
  })
})

describe('listing and revoking', () => {
  it('loads the list and marks it loaded', async () => {
    get.mockResolvedValue(ok([token(), token({ id: '2', name: 'other' })]))

    const store = useTokens()
    await store.load()

    expect(store.loaded.value).toBe(true)
    expect(store.tokens.value.map((t) => t.name)).toEqual(['laptop agent', 'other'])
  })

  it('reports a failed load without pretending the list is empty by choice', async () => {
    get.mockResolvedValue(refusal(401, { code: 'auth.required', message: 'Sign in first.' }))

    const store = useTokens()
    await store.load()

    expect(store.loaded.value).toBe(false)
    expect(store.failure.value?.kind).toBe('unauthorised')
  })

  it('revokes and reloads', async () => {
    del.mockResolvedValue({ error: undefined, response: { status: 204, ok: true } })
    get.mockResolvedValue(ok([token({ revokedAt: '2026-09-22T01:00:00Z' })]))

    const store = useTokens()
    await store.revoke('00000000-0000-0000-0000-000000000010')

    expect(del).toHaveBeenCalledWith('/tokens/{tokenId}', {
      params: { path: { tokenId: '00000000-0000-0000-0000-000000000010' } },
    })
    expect(store.tokens.value[0]?.revokedAt).toBe('2026-09-22T01:00:00Z')
  })

  it('reports a revoke the server refused', async () => {
    del.mockResolvedValue({
      error: { code: 'token.not_found', message: 'No token of yours has that id.' },
      response: { status: 404, ok: false },
    })

    const store = useTokens()
    await store.revoke('nope')

    expect(store.failure.value?.kind).toBe('error')
    expect(get).not.toHaveBeenCalled()
  })

  // The network, not the server: a request that never answered is a failure
  // the page has to show, not a promise that hangs.
  it('reports an unreachable server', async () => {
    get.mockRejectedValue(new TypeError('Failed to fetch'))

    const store = useTokens()
    await store.load()

    expect(store.failure.value).not.toBeNull()
    expect(store.busy.value).toBe(false)
  })
})
