import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()
const del = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post, PUT: put, DELETE: del } }
})

const { useServiceAccounts, tokenState, liveTokens, maxLiveTokens } = await import(
  './useServiceAccounts'
)

const now = Date.parse('2026-09-24T12:00:00Z')

function token(overrides: Record<string, unknown> = {}) {
  return {
    id: '00000000-0000-0000-0000-000000000010',
    name: 'blue',
    createdAt: '2026-09-20T00:00:00Z',
    expiresAt: '2026-10-20T00:00:00Z',
    lastUsedAt: null,
    revokedAt: null,
    ...overrides,
  }
}

function account(overrides: Record<string, unknown> = {}) {
  return {
    id: '00000000-0000-0000-0000-000000000020',
    username: 'svc-reporter',
    description: 'Reporter, answering #data-help.',
    disabled: false,
    createdBy: 'avior_admin',
    createdAt: '2026-09-20T00:00:00Z',
    roles: [],
    tokens: [],
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
  put.mockReset()
  del.mockReset()
})

describe('which tokens are live', () => {
  it('reads a revoked token as revoked whatever its expiry', () => {
    expect(tokenState(token({ revokedAt: '2026-09-21T00:00:00Z' }), now)).toBe('revoked')
  })

  it('reads a token past its expiry as expired', () => {
    expect(tokenState(token({ expiresAt: '2026-09-24T11:59:59Z' }), now)).toBe('expired')
  })

  // AC-5 counts live tokens only, so an expired or revoked one never stands
  // between an administrator and a rotation.
  it('counts only live ones against the limit', () => {
    const held = account({
      tokens: [
        token({ id: 'a' }),
        token({ id: 'b', revokedAt: '2026-09-21T00:00:00Z' }),
        token({ id: 'c', expiresAt: '2026-09-01T00:00:00Z' }),
      ],
    })
    expect(liveTokens(held, now).map((t) => t.id)).toEqual(['a'])
    expect(maxLiveTokens).toBe(2)
  })
})

describe('creating', () => {
  it('sends the name and the description and reloads the list', async () => {
    post.mockResolvedValue(ok(account(), 201))
    get.mockResolvedValue(ok([account()]))
    const page = useServiceAccounts()

    expect(await page.create('svc-reporter', 'Reporter')).toBe(true)
    expect(post).toHaveBeenCalledWith('/service-accounts', {
      body: { name: 'svc-reporter', description: 'Reporter' },
    })
    expect(page.accounts.value).toHaveLength(1)
  })

  it('keeps the refusal for the banner', async () => {
    post.mockResolvedValue(refusal(422, { code: 'service_account.name_taken', message: 'taken' }))
    const page = useServiceAccounts()

    expect(await page.create('svc-reporter', 'Reporter')).toBe(false)
    expect(page.failure.value).not.toBeNull()
    expect(get).not.toHaveBeenCalled()
  })

  it('reports a server that cannot be reached', async () => {
    post.mockRejectedValue(new Error('offline'))
    const page = useServiceAccounts()

    expect(await page.create('svc-reporter', 'Reporter')).toBe(false)
    expect(page.failure.value).not.toBeNull()
    expect(page.busy.value).toBe(false)
  })
})

/*
 * The value appears once. It is held apart from the list, which never gains
 * one, and dismissing it is final.
 */
describe('minting', () => {
  it('holds the value apart from the list', async () => {
    post.mockResolvedValue(ok({ ...token(), token: 'avior_secret' }, 201))
    get.mockResolvedValue(ok([account({ tokens: [token()] })]))
    const page = useServiceAccounts()

    expect(await page.mint(account(), 'blue', 30)).toBe(true)
    expect(post).toHaveBeenCalledWith('/service-accounts/{operatorId}/tokens', {
      params: { path: { operatorId: account().id } },
      body: { name: 'blue', expiresInDays: 30 },
    })
    expect(page.minted.value?.token.token).toBe('avior_secret')
    expect(page.minted.value?.account).toBe('svc-reporter')
    expect(JSON.stringify(page.accounts.value)).not.toContain('avior_secret')

    page.dismissMinted()
    expect(page.minted.value).toBeNull()
  })

  it('leaves nothing to show when a third is refused', async () => {
    post.mockResolvedValue(refusal(409, { code: 'token.too_many_live', message: 'two live' }))
    const page = useServiceAccounts()

    expect(await page.mint(account(), 'third', 30)).toBe(false)
    expect(page.minted.value).toBeNull()
    expect(page.failure.value).not.toBeNull()
  })
})

describe('revoking and switching off', () => {
  it('revokes one token and reloads', async () => {
    del.mockResolvedValue({ error: undefined, response: { status: 204, ok: true } })
    get.mockResolvedValue(ok([account()]))
    const page = useServiceAccounts()

    expect(await page.revoke(account(), token())).toBe(true)
    expect(del).toHaveBeenCalledWith('/service-accounts/{operatorId}/tokens/{tokenId}', {
      params: { path: { operatorId: account().id, tokenId: token().id } },
    })
  })

  it('reports a revoke the server refused', async () => {
    del.mockResolvedValue({
      error: { code: 'token.not_found', message: 'gone' },
      response: { status: 404, ok: false },
    })
    const page = useServiceAccounts()

    expect(await page.revoke(account(), token())).toBe(false)
    expect(page.failure.value).not.toBeNull()
  })

  // The account page's own endpoint: switching a service account off is the
  // same act, under the same rule and the same record, as anybody's.
  it('switches an account off through the account page endpoint', async () => {
    put.mockResolvedValue(ok({ ...account(), disabled: true, kind: 'service' }))
    get.mockResolvedValue(ok([account({ disabled: true })]))
    const page = useServiceAccounts()

    expect(await page.setDisabled(account(), true)).toBe(true)
    expect(put).toHaveBeenCalledWith('/operators/{operatorId}/disabled', {
      params: { path: { operatorId: account().id } },
      body: { disabled: true },
    })
    expect(page.accounts.value[0]?.disabled).toBe(true)
  })

  it('reports a switch the server refused', async () => {
    put.mockResolvedValue(refusal(403, { code: 'permission.denied', message: 'no' }))
    const page = useServiceAccounts()

    expect(await page.setDisabled(account(), true)).toBe(false)
    expect(page.failure.value).not.toBeNull()
  })
})

describe('loading', () => {
  it('reports a list the server refused', async () => {
    get.mockResolvedValue(refusal(403, { code: 'permission.denied', message: 'no' }))
    const page = useServiceAccounts()

    expect(await page.load()).toBe(false)
    expect(page.loaded.value).toBe(false)
    expect(page.failure.value).not.toBeNull()
  })
})
