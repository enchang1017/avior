import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const del = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, DELETE: del, POST: post } }
})

const { useWriteCaptures, describeAge } = await import('./useWriteCaptures')

function capture(id: string, ageHours: number, stale: boolean, rolledBackBy?: string) {
  return {
    request: {
      id,
      database: 'aviorfixture',
      statement: 'UPDATE public.bet SET status = $$',
      kind: 'update' as const,
      targetTable: 'public.bet',
      reason: 'why',
      requestedBy: 'dev',
      requestedAt: '2026-09-01T02:00:00Z',
      estimatedRows: 1,
      state: 'applied' as const,
      reversible: true,
      captureRows: 3,
      reverseStatement: 'UPDATE public.bet AS t ...',
      ...(rolledBackBy === undefined ? {} : { rolledBackBy }),
    },
    ageHours,
    stale,
  }
}

function ok(body: unknown) {
  return { data: body, error: undefined, response: { status: 200 } }
}

beforeEach(() => {
  get.mockReset()
  del.mockReset()
  post.mockReset()
})

describe('describeAge', () => {
  it('says how old in the words somebody reads', () => {
    expect(describeAge(0)).toBe('just now')
    expect(describeAge(1)).toBe('1 hour ago')
    expect(describeAge(5)).toBe('5 hours ago')
    expect(describeAge(24)).toBe('1 day ago')
    expect(describeAge(200)).toBe('8 days ago')
  })
})

describe('useWriteCaptures', () => {
  it('counts what is marked and can show only those', async () => {
    get.mockResolvedValue(
      ok([capture('a', 200, true), capture('b', 2, false), capture('c', 400, true)]),
    )

    const page = useWriteCaptures()
    await page.load()

    expect(page.staleCount.value).toBe(2)
    expect(page.shown.value).toHaveLength(3)

    page.staleOnly.value = true
    expect(page.shown.value.map((item) => item.request.id)).toEqual(['a', 'c'])
  })

  // The row leaves the page rather than the system: the request keeps its
  // record and reads afterwards as no longer reversible.
  it('drops the removed capture from the list it is shown in', async () => {
    get.mockResolvedValue(ok([capture('a', 200, true), capture('b', 2, false)]))
    del.mockResolvedValue(ok({ request: capture('a', 200, true).request, history: [] }))

    const page = useWriteCaptures()
    await page.load()

    expect(await page.remove('a')).toBe(true)
    expect(page.captures.value.map((item) => item.request.id)).toEqual(['b'])
  })

  it('reads a capture somebody already removed as a conflict', async () => {
    get.mockResolvedValue(ok([capture('a', 200, true)]))
    del.mockResolvedValue({
      data: undefined,
      error: { code: 'write.capture_gone', message: 'this request has no capture to remove' },
      response: { status: 409 },
    })

    const page = useWriteCaptures()
    await page.load()

    expect(await page.remove('a')).toBe(false)
    expect(page.failure.value).toBeNull()
    expect(page.conflict.value?.code).toBe('write.capture_gone')
    // Still listed: nothing was removed, so pretending otherwise would leave
    // the reader with a page that disagrees with the database.
    expect(page.captures.value).toHaveLength(1)
  })

  /*
   * The row leaves this page once the rows are back: its backup belongs to the
   * rollback now, and the request it undid cannot be undone twice.
   */
  it('reads the list again after putting rows back, and keeps a refusal as a conflict', async () => {
    get.mockResolvedValue(ok([capture('a', 10, false), capture('b', 2, false)]))
    post.mockResolvedValue(ok({ request: capture('a', 10, false).request, history: [] }))

    const page = useWriteCaptures()
    await page.load()

    // The list is read again rather than filtered: a rollback keeps a backup of
    // its own, so undoing one both marks a row here and adds a new one.
    get.mockResolvedValue(ok([capture('b', 2, false), capture('c', 0, false)]))

    expect(await page.undo('a', 'settled in error')).toBe(true)
    expect(post).toHaveBeenCalledWith('/writes/{writeId}/rollback', {
      params: { path: { writeId: 'a' } },
      body: { reason: 'settled in error' },
    })
    expect(page.captures.value.map((item) => item.request.id)).toEqual(['b', 'c'])

    post.mockResolvedValue({
      data: undefined,
      error: { code: 'write.not_undoable', message: 'this request has already been undone' },
      response: { status: 409 },
    })
    expect(await page.undo('b', 'again')).toBe(false)
    expect(page.failure.value).toBeNull()
    expect(page.conflict.value?.code).toBe('write.not_undoable')
    // Nothing was read again, because nothing changed: the list still holds
    // what the reload before it returned.
    expect(page.captures.value.map((item) => item.request.id)).toEqual(['b', 'c'])
  })

  it('reports an unreachable server rather than throwing at the view', async () => {
    get.mockRejectedValue(new TypeError('failed to fetch'))

    const page = useWriteCaptures()
    await page.load()

    expect(page.failure.value).not.toBeNull()
    expect(page.busy.value).toBe(false)
  })
})

/*
 * A rollback does not remove the capture it read: the rows stay, and a DBA may
 * still want to prune them. So a request that was already undone goes on
 * appearing on that page — and offering Undo on one is a control certain to be
 * refused, which is exactly what it did.
 */
describe('a capture whose change was already put back', () => {
  it('is still listed, because its rows are still there to prune', async () => {
    const undone = capture('a', 10, false, '00000000-0000-0000-0000-0000000000ff')
    get.mockResolvedValue(ok([undone]))

    const page = useWriteCaptures()
    await page.load()

    expect(page.captures.value).toHaveLength(1)
    expect(page.captures.value[0]?.request).toHaveProperty('rolledBackBy')
  })
})
