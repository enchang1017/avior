import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { useWriteRequests, rollbackWrite, isLarge, isSettled, scopeStates } = await import(
  './useWriteRequests'
)

const waiting = {
  id: '00000000-0000-0000-0000-0000000000w1',
  database: 'aviorfixture',
  statement: "UPDATE public.bet SET status = 'Void' WHERE bet_id = 1",
  kind: 'update' as const,
  targetTable: 'public.bet',
  reason: 'a stuck settlement',
  requestedBy: 'dev',
  requestedAt: '2026-09-09T02:00:00Z',
  estimatedRows: 1,
  state: 'pending' as const,
  reversible: false,
}

function ok(body: unknown) {
  return { data: body, error: undefined, response: { status: 200 } }
}

function fail(status: number, body: unknown) {
  return { data: undefined, error: body, response: { status } }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
})

describe('isLarge', () => {
  it('calls out an estimate an approver should not skim past', () => {
    expect(isLarge(10)).toBe(false)
    expect(isLarge(1000)).toBe(false)
    expect(isLarge(1001)).toBe(true)
  })
})

describe('isSettled', () => {
  it('treats everything but pending as decided', () => {
    expect(isSettled('pending')).toBe(false)
    for (const state of ['applied', 'failed', 'rejected', 'withdrawn'] as const) {
      expect(isSettled(state)).toBe(true)
    }
  })
})

describe('scopeStates', () => {
  // The queue is what is waiting. A settled request in it would make "nothing
  // needs you" stop being a readable answer.
  it('narrows the queue to what is waiting', () => {
    expect(scopeStates.queue).toEqual(['pending'])
    expect(scopeStates.mine).toContain('applied')
  })
})

describe('rollbackWrite', () => {
  it('hands back the detail, the conflict, or neither', async () => {
    post.mockResolvedValueOnce(ok({ request: waiting, history: [] }))
    expect((await rollbackWrite(waiting.id, 'why')).detail).not.toBeUndefined()

    post.mockResolvedValueOnce(fail(422, { code: 'write.reason_missing', message: 'no' }))
    expect((await rollbackWrite(waiting.id, '')).conflict?.code).toBe('write.reason_missing')

    // A 500 is not the operator's to act on, so it comes back as neither and
    // the caller reports it the way it reports any other fault.
    post.mockResolvedValueOnce(fail(500, undefined))
    const broken = await rollbackWrite(waiting.id, 'why')
    expect(broken.detail).toBeUndefined()
    expect(broken.conflict).toBeUndefined()
    expect(broken.status).toBe(500)
  })
})

describe('useWriteRequests', () => {
  it('asks the queue only for what is waiting, and its own list for everything', async () => {
    get.mockResolvedValue(ok([waiting]))

    const queue = useWriteRequests(() => 'queue')
    await queue.load()
    expect(get).toHaveBeenCalledWith('/writes', {
      params: { query: { limit: 100, state: 'pending' } },
    })

    get.mockClear()
    const mine = useWriteRequests(() => 'mine')
    await mine.load()
    expect(get).toHaveBeenCalledWith('/writes', { params: { query: { limit: 100 } } })
  })

  // A refusal is the feature working. Reporting it as a page failure would tell
  // the requester Avior broke when what happened is that the guard answered.
  it('keeps a guard refusal apart from a failure of the page', async () => {
    post.mockResolvedValue(
      fail(422, {
        code: 'guard.refused',
        message: 'the guard refused this statement',
        reasons: [{ rule: 'missing_where_clause', message: 'UPDATE must carry a WHERE clause' }],
      }),
    )

    const page = useWriteRequests()
    const made = await page.submit('aviorfixture', 'UPDATE public.bet SET status = $$', 'why')

    expect(made).toBeNull()
    expect(page.failure.value).toBeNull()
    expect(page.refusal.value?.reasons.at(0)?.rule).toBe('missing_where_clause')
  })

  it('reloads the list after a submission is recorded', async () => {
    post.mockResolvedValue(ok(waiting))
    get.mockResolvedValue(ok([waiting]))

    const page = useWriteRequests()
    const made = await page.submit('aviorfixture', waiting.statement, 'why')

    expect(made?.id).toBe(waiting.id)
    expect(get).toHaveBeenCalledTimes(1)
    expect(page.requests.value).toHaveLength(1)
  })

  it('sends a note only when there is one', async () => {
    post.mockResolvedValue(ok({ request: waiting, history: [] }))
    get.mockResolvedValue(ok([]))

    const page = useWriteRequests()
    await page.decide(waiting.id, 'approve', '')
    expect(post).toHaveBeenCalledWith('/writes/{writeId}/decision', {
      params: { path: { writeId: waiting.id } },
      body: { decision: 'approve' },
    })

    post.mockClear()
    await page.decide(waiting.id, 'reject', 'too many rows')
    expect(post).toHaveBeenCalledWith('/writes/{writeId}/decision', {
      params: { path: { writeId: waiting.id } },
      body: { decision: 'reject', note: 'too many rows' },
    })
  })

  // A request somebody else already decided is not a fault: it is the answer,
  // and the reader needs to see it rather than a banner about Avior.
  it('reads a conflict as a conflict rather than a failure', async () => {
    post.mockResolvedValue(
      fail(409, { code: 'write.settled', message: 'this request has already been decided' }),
    )

    const page = useWriteRequests()
    await page.decide(waiting.id, 'approve', '')

    expect(page.failure.value).toBeNull()
    expect(page.conflict.value?.code).toBe('write.settled')
  })

  /*
   * Undoing is a write of its own, so the page reloads: the rollback is a new
   * row in the same list, and a list that did not show it would be describing a
   * state that lasted one request.
   */
  it('reloads the list after a rollback, and keeps a refusal as a conflict', async () => {
    post.mockResolvedValue(ok({ request: waiting, history: [] }))
    get.mockResolvedValue(ok([waiting]))

    const page = useWriteRequests()
    await page.rollback(waiting.id, 'settled in error')
    expect(post).toHaveBeenCalledWith('/writes/{writeId}/rollback', {
      params: { path: { writeId: waiting.id } },
      body: { reason: 'settled in error' },
    })
    expect(get).toHaveBeenCalledTimes(1)

    post.mockResolvedValue(
      fail(409, { code: 'write.not_undoable', message: 'this request has already been undone' }),
    )
    await page.rollback(waiting.id, 'again')
    expect(page.failure.value).toBeNull()
    expect(page.conflict.value?.code).toBe('write.not_undoable')
  })

  it('reports an unreachable server rather than throwing at the view', async () => {
    get.mockRejectedValue(new TypeError('failed to fetch'))

    const page = useWriteRequests()
    await page.load()

    expect(page.failure.value).not.toBeNull()
    expect(page.busy.value).toBe(false)
  })

  it('clears the detail and every message when the reader closes it', async () => {
    get.mockResolvedValue(ok({ request: waiting, history: [] }))

    const page = useWriteRequests()
    await page.open(waiting.id)
    expect(page.detail.value).not.toBeNull()

    page.close()
    expect(page.detail.value).toBeNull()
    expect(page.refusal.value).toBeNull()
  })

  // FEAT-074 AC-6: the detail is busy from the moment it is asked to open.
  it('says which request is opening until its detail answers', async () => {
    let answer: (value: unknown) => void = () => undefined
    get.mockReturnValue(new Promise((resolve) => (answer = resolve)))

    const page = useWriteRequests()
    const opened = page.open(waiting.id)
    expect(page.opening.value).toBe(waiting.id)
    expect(page.busy.value).toBe(false)

    answer(ok({ request: waiting, history: [] }))
    await opened
    expect(page.opening.value).toBe('')
    expect(page.detail.value?.request.id).toBe(waiting.id)
  })

  it('drops a detail that answers after the reader moved on', async () => {
    let answer: (value: unknown) => void = () => undefined
    get.mockReturnValue(new Promise((resolve) => (answer = resolve)))

    const page = useWriteRequests()
    const opened = page.open(waiting.id)
    page.close()
    answer(ok({ request: waiting, history: [] }))
    await opened

    expect(page.opening.value).toBe('')
    expect(page.detail.value).toBeNull()
  })

  /*
   * Working down a queue goes back and forth. A request read once is shown
   * again at once, with no request and no loader, until the list is loaded
   * again: a reload is somebody asking for what is true now.
   */
  it('shows a request read before without asking again, until the list is reloaded', async () => {
    const other = { ...waiting, id: '00000000-0000-0000-0000-0000000000w2' }
    get.mockImplementation((path: string, init: { params: { path?: { writeId: string } } }) =>
      Promise.resolve(
        path === '/writes'
          ? ok([waiting, other])
          : ok({ request: init.params.path?.writeId === other.id ? other : waiting, history: [] }),
      ),
    )
    const page = useWriteRequests()
    const details = () => get.mock.calls.filter(([path]) => path === '/writes/{writeId}').length

    await page.open(waiting.id)
    await page.open(other.id)
    const back = page.open(waiting.id)
    expect(page.opening.value).toBe('')
    expect(page.detail.value?.request.id).toBe(waiting.id)
    await back
    expect(details()).toBe(2)

    await page.load()
    await page.open(other.id)
    expect(details()).toBe(3)
  })

  it('reads the open request again on a refresh, keeping it on screen meanwhile', async () => {
    get.mockResolvedValueOnce(ok({ request: waiting, history: [] }))
    const page = useWriteRequests()
    await page.open(waiting.id)

    let answer: (value: unknown) => void = () => undefined
    get.mockResolvedValueOnce(ok([waiting]))
    get.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)))
    const refreshed = page.refresh()
    await vi.waitFor(() => expect(get).toHaveBeenCalledTimes(3))
    expect(page.opening.value).toBe('')
    expect(page.detail.value?.request.state).toBe('pending')

    answer(ok({ request: { ...waiting, state: 'applied' }, history: [] }))
    await refreshed
    expect(page.detail.value?.request.state).toBe('applied')
  })

  it('keeps what a decision answered, rather than what was read before it', async () => {
    get.mockResolvedValueOnce(ok({ request: waiting, history: [] }))
    const page = useWriteRequests()
    await page.open(waiting.id)

    post.mockResolvedValueOnce(ok({ request: { ...waiting, state: 'rejected' }, history: [] }))
    get.mockResolvedValueOnce(ok([]))
    await page.decide(waiting.id, 'reject', '')
    expect(page.detail.value?.request.state).toBe('rejected')

    page.close()
    await page.open(waiting.id)
    expect(page.detail.value?.request.state).toBe('rejected')
    expect(get).toHaveBeenCalledTimes(2)
  })
})
