import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { useReleaseQueue, driftBlocks, isSettled, scopeStates, requestedPageSize } = await import(
  './useReleaseQueue'
)

const pending = {
  id: '00000000-0000-0000-0000-0000000000r1',
  database: 'aviordeploy',
  commitSha: '0f1c2d3e4f5061728394a5b6c7d8e9f0a1b2c3d4',
  author: 'ci@example',
  migrationChecksum: 'abc123',
  migrationCount: 2,
  state: 'pending' as const,
  submittedAt: '2026-09-03T02:00:00Z',
}

function page(releases: unknown[], pageSize = requestedPageSize, hasMore = false) {
  return { data: { releases, pageSize, hasMore }, error: undefined, response: { status: 200 } }
}

function detail(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      release: pending,
      migrations: [{ filename: '001.sql', sql: 'ALTER TABLE public.bet ADD COLUMN note text;' }],
      lintFindings: [],
      driftCheck: null,
      approvals: [],
      applications: [],
      ...overrides,
    },
    error: undefined,
    response: { status: 200 },
  }
}

function refusal(status: number, body: unknown) {
  return { data: undefined, error: body, response: { status } }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
})

describe('driftBlocks', () => {
  it('treats a match as the only result that permits an apply', () => {
    expect(driftBlocks({ result: 'match', checkedAt: 'x' })).toBe(false)
    expect(driftBlocks({ result: 'drifted', checkedAt: 'x' })).toBe(true)
  })

  // The one that matters. A digest that could not be computed tells nobody
  // anything, and rendering it as an absence is how somebody approves against a
  // database nobody looked at.
  it('treats an unavailable check as blocking, not as an absence', () => {
    expect(driftBlocks({ result: 'unavailable', checkedAt: 'x' })).toBe(true)
  })

  it('does not block when no check has run yet', () => {
    expect(driftBlocks(null)).toBe(false)
    expect(driftBlocks(undefined)).toBe(false)
  })
})

describe('isSettled', () => {
  it('is true for every state but pending', () => {
    expect(isSettled('pending')).toBe(false)
    for (const state of ['approved', 'rejected', 'applied', 'failed'] as const) {
      expect(isSettled(state)).toBe(true)
    }
  })
})

describe('useReleaseQueue', () => {
  // Together the two lists cover every state exactly once. A state in neither
  // would be invisible; a state in both would be counted twice.
  it('partitions the states between the two lists', () => {
    const all = [...scopeStates.pending, ...scopeStates.history].sort()
    expect(all).toEqual(['applied', 'approved', 'failed', 'pending', 'rejected'])
  })

  it('asks only for the states its list covers', async () => {
    get.mockResolvedValue(page([pending]))
    const queue = useReleaseQueue(() => 'pending')
    await queue.load()

    const asked = get.mock.calls.map((call) => call[1].params.query.state).sort()
    expect(asked).toEqual(['approved', 'pending'])
  })

  it('narrows to one state when a filter is set', async () => {
    get.mockResolvedValue(page([pending]))
    const queue = useReleaseQueue(() => 'history')
    queue.state.value = 'applied'
    queue.database.value = ' aviordeploy '
    await queue.applyFilters()

    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenLastCalledWith('/releases', {
      params: {
        query: { limit: requestedPageSize, offset: 0, state: 'applied', database: 'aviordeploy' },
      },
    })
  })

  // A limit above the server's maximum comes back clamped. Stepping by the size
  // that was asked for would skip every row between the two.
  it('pages by the size the response reports, not the one requested', async () => {
    get.mockResolvedValue(page([pending], 25, true))
    const queue = useReleaseQueue(() => 'history')
    queue.state.value = 'applied'
    await queue.load()

    await queue.next()
    expect(get).toHaveBeenLastCalledWith('/releases', {
      params: { query: { limit: requestedPageSize, offset: 25, state: 'applied' } },
    })
  })

  it('does not step past the last page', async () => {
    get.mockResolvedValue(page([pending], requestedPageSize, false))
    const queue = useReleaseQueue(() => 'pending')
    await queue.load()

    const calls = get.mock.calls.length
    await queue.next()
    expect(get.mock.calls.length).toBe(calls)
  })

  // One refused request refuses the page. A list assembled from a subset of the
  // states it covers is not the list, and showing it would be a wrong answer
  // rather than a partial one.
  it('clears a stale page when any request is refused', async () => {
    get.mockResolvedValue(page([pending]))
    const queue = useReleaseQueue(() => 'pending')
    await queue.load()
    expect(queue.releases.value.length).toBeGreaterThan(0)

    get.mockReset()
    get.mockResolvedValueOnce(page([pending]))
    get.mockResolvedValueOnce(refusal(403, { code: 'x', message: 'no' }))
    await queue.load()
    expect(queue.releases.value).toHaveLength(0)
    expect(queue.forbidden.value).toBe(true)
  })

  it('reads one release with its SQL', async () => {
    get.mockResolvedValue(detail())
    const queue = useReleaseQueue()
    await queue.open(pending.id)

    expect(get).toHaveBeenCalledWith('/releases/{releaseId}', {
      params: { path: { releaseId: pending.id } },
    })
    expect(queue.detail.value?.migrations[0]?.sql).toContain('ALTER TABLE')
  })

  it('surfaces a blocking drift check on the entry being read', async () => {
    get.mockResolvedValue(
      detail({ driftCheck: { result: 'unavailable', checkedAt: '2026-09-03T02:01:00Z' } }),
    )
    const queue = useReleaseQueue()
    await queue.open(pending.id)

    expect(queue.driftIsBlocking.value).toBe(true)
  })

  it('sends a decision with its note, and omits an empty one', async () => {
    post.mockResolvedValue(detail())
    const queue = useReleaseQueue()

    await queue.decide(pending.id, 'approve', '  looks right  ')
    expect(post).toHaveBeenLastCalledWith('/releases/{releaseId}/decision', {
      params: { path: { releaseId: pending.id } },
      body: { decision: 'approve', note: 'looks right' },
    })

    await queue.decide(pending.id, 'reject', '   ')
    expect(post).toHaveBeenLastCalledWith('/releases/{releaseId}/decision', {
      params: { path: { releaseId: pending.id } },
      body: { decision: 'reject' },
    })
  })

  // Each 409 code means something an approver can act on. Collapsing them into
  // one error message would leave them guessing which happened.
  it('keeps the three conflicts distinguishable', async () => {
    const queue = useReleaseQueue()
    for (const code of ['release.settled', 'release.not_approved', 'release.drift_detected']) {
      post.mockResolvedValueOnce(refusal(409, { code, message: `because ${code}` }))
      await queue.apply(pending.id)
      expect(queue.conflict.value?.code).toBe(code)
      expect(queue.failure.value).toBeNull()
    }
  })

  it('separates a permission refusal from a conflict', async () => {
    post.mockResolvedValue(refusal(403, { code: 'release.forbidden', message: 'no' }))
    const queue = useReleaseQueue()
    await queue.apply(pending.id)

    expect(queue.forbidden.value).toBe(true)
    expect(queue.conflict.value).toBeNull()
  })

  // A migration that failed against the target answers 502 with the record.
  // Avior did its job; the target refused the change, and the detail says how
  // far it got. Throwing it away would hide the only account of that.
  it('keeps the record when applying failed against the target', async () => {
    post.mockResolvedValue(
      refusal(502, {
        release: { ...pending, state: 'failed' },
        migrations: [],
        lintFindings: [],
        driftCheck: null,
        approvals: [],
        applications: [
          {
            appliedBy: 'dba',
            startedAt: '2026-09-03T02:05:00Z',
            result: 'failed',
            filesApplied: 1,
            error: 'relation "no_such_table" does not exist',
          },
        ],
      }),
    )
    const queue = useReleaseQueue()
    await queue.apply(pending.id)

    expect(queue.detail.value?.release.state).toBe('failed')
    expect(queue.detail.value?.applications[0]?.error).toContain('no_such_table')
    expect(queue.failure.value).toBeNull()
  })
})
