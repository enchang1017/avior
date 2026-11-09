import { nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { useAuditLog, requestedPageSize } = await import('./useAuditLog')

const executed = {
  id: '00000000-0000-0000-0000-00000000ae01',
  operator: 'lin.wei',
  instanceId: '00000000-0000-0000-0000-0000000000a1',
  database: 'aviorfixture',
  statement: 'SELECT member_id FROM public.member',
  decision: 'allow' as const,
  reasons: [],
  rowCount: 12,
  elapsedMs: 45,
  createdAt: '2025-08-30T09:15:00Z',
}

const denied = {
  id: '00000000-0000-0000-0000-00000000ae02',
  operator: 'chen.yu',
  instanceId: '00000000-0000-0000-0000-0000000000a1',
  database: 'aviorfixture',
  statement: 'SELECT * FROM public.member',
  decision: 'deny' as const,
  reasons: [
    { rule: 'wildcard_select', message: 'SELECT * is not permitted; name the columns explicitly.' },
    {
      rule: 'column_denied',
      message: 'caller is not permitted to read this column',
      subject: 'public.member.id_card_number',
    },
  ],
  rowCount: null,
  elapsedMs: null,
  createdAt: '2025-08-30T09:10:00Z',
}

function ok(entries: unknown[], pageSize: number, hasMore: boolean) {
  return { data: { entries, pageSize, hasMore }, response: { status: 200 } }
}

function answers(pageSize = requestedPageSize, hasMore = false) {
  get.mockResolvedValue(ok([executed, denied], pageSize, hasMore))
}

function refused(status: number, code: string, message: string) {
  get.mockResolvedValue({
    data: undefined,
    error: { code, message },
    response: { status },
  })
}

function lastQuery(): Record<string, unknown> {
  const call = get.mock.calls.at(-1)
  if (call === undefined) throw new Error('no request was made')
  const options = call[1] as { params: { query: Record<string, unknown> } }
  return options.params.query
}

describe('useAuditLog', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it("reads the caller's own log by default", async () => {
    answers()
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(get).toHaveBeenCalledWith('/audit/mine', {
      params: { query: { limit: requestedPageSize, offset: 0 } },
    })
    expect(log.entries.value).toEqual([executed, denied])
  })

  // AC-3. No parameter widens the personal log, so the administrator's filters
  // are not sent to it even when they hold a value.
  it('never sends an operator filter to the personal log', async () => {
    answers()
    const log = useAuditLog(() => 'mine')
    log.operator.value = 'chen.yu'
    log.statement.value = 'member'
    log.decision.value = 'deny'

    await log.load()

    expect(lastQuery()).toEqual({ limit: requestedPageSize, offset: 0 })
  })

  it("addresses the administrator's log in the wider scope", async () => {
    answers()
    const log = useAuditLog(() => 'all')

    await log.load()

    expect(get).toHaveBeenCalledWith('/audit', {
      params: { query: { limit: requestedPageSize, offset: 0 } },
    })
  })

  // AC-6. The filters combine rather than replacing one another.
  it("combines the administrator's filters", async () => {
    answers()
    const log = useAuditLog(() => 'all')
    log.operator.value = 'chen'
    log.statement.value = 'member'
    log.decision.value = 'deny'

    await log.load()

    expect(lastQuery()).toMatchObject({
      operator: 'chen',
      statement: 'member',
      decision: 'deny',
    })
  })

  it('omits a filter left blank rather than sending an empty one', async () => {
    answers()
    const log = useAuditLog(() => 'all')
    log.operator.value = '   '
    log.statement.value = ''

    await log.load()

    expect(lastQuery()).toEqual({ limit: requestedPageSize, offset: 0 })
  })

  // AC-4.
  it('sends the date range as instants', async () => {
    answers()
    const log = useAuditLog(() => 'mine')
    log.from.value = '2025-08-01T00:00'
    log.to.value = '2025-08-31T23:59'

    await log.load()

    expect(lastQuery()).toMatchObject({
      from: new Date('2025-08-01T00:00').toISOString(),
      to: new Date('2025-08-31T23:59').toISOString(),
    })
  })

  it('bounds nothing with a range that will not parse', async () => {
    answers()
    const log = useAuditLog(() => 'mine')
    log.from.value = 'last August'

    await log.load()

    expect(lastQuery()).toEqual({ limit: requestedPageSize, offset: 0 })
  })

  // AC-10. The requested limit is not the applied one.
  it('carries the page size the response reports, not the one asked for', async () => {
    answers(200)
    const log = useAuditLog(() => 'mine')
    log.limit.value = 500

    await log.load()

    expect(lastQuery()).toMatchObject({ limit: 500 })
    expect(log.pageSize.value).toBe(200)
  })

  it('steps by the applied page size when paging forward', async () => {
    answers(200, true)
    const log = useAuditLog(() => 'mine')
    log.limit.value = 500
    await log.load()

    await log.next()

    expect(lastQuery()).toMatchObject({ limit: 500, offset: 200 })
    expect(log.page.value).toBe(2)
  })

  it('does not page past the last page', async () => {
    answers(50, false)
    const log = useAuditLog(() => 'mine')
    await log.load()

    await log.next()

    expect(get).toHaveBeenCalledTimes(1)
    expect(log.offset.value).toBe(0)
  })

  it('pages back and stops at the first page', async () => {
    answers(50, true)
    const log = useAuditLog(() => 'mine')
    await log.load()
    await log.next()

    await log.previous()
    expect(log.offset.value).toBe(0)
    expect(log.canGoBack.value).toBe(false)

    await log.previous()
    expect(get).toHaveBeenCalledTimes(3)
  })

  it('returns to the first page when the filters are applied', async () => {
    answers(50, true)
    const log = useAuditLog(() => 'mine')
    await log.load()
    await log.next()

    log.from.value = '2025-08-01T00:00'
    await log.apply()

    expect(log.offset.value).toBe(0)
    expect(lastQuery()).toMatchObject({ offset: 0 })
  })

  // Two routes over one component: the instance is reused, so the scope has to
  // be followed rather than captured.
  it('follows the scope when the operator switches log', async () => {
    answers()
    const scope = ref<'mine' | 'all'>('mine')
    const log = useAuditLog(() => scope.value)
    await log.load()

    scope.value = 'all'
    await nextTick()
    await Promise.resolve()

    expect(get).toHaveBeenLastCalledWith('/audit', {
      params: { query: { limit: requestedPageSize, offset: 0 } },
    })
  })

  it('drops the administrator filters and the offset on the way back', async () => {
    answers(50, true)
    const scope = ref<'mine' | 'all'>('all')
    const log = useAuditLog(() => scope.value)
    log.operator.value = 'chen'
    log.decision.value = 'deny'
    await log.load()
    await log.next()

    scope.value = 'mine'
    await nextTick()
    await Promise.resolve()

    expect(log.operator.value).toBe('')
    expect(log.decision.value).toBe('')
    expect(log.offset.value).toBe(0)
  })

  // AC-7. A refused administrator's view is its own state: the view offers the
  // operator their own log rather than a bare error line.
  it("surfaces a refusal of the administrator's view as forbidden", async () => {
    refused(403, 'auth.forbidden', 'You may not read other operators submissions.')
    const log = useAuditLog(() => 'all')

    await log.load()

    expect(log.forbidden.value).toBe(true)
    expect(log.failure.value).toEqual({
      kind: 'error',
      code: 'auth.forbidden',
      message: 'You may not read other operators submissions.',
    })
    expect(log.entries.value).toEqual([])
  })

  it('surfaces an inverted range as a validation failure, not a refusal', async () => {
    refused(422, 'audit.range_inverted', 'The end of the range precedes its start.')
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(log.forbidden.value).toBe(false)
    expect(log.failure.value).toEqual({
      kind: 'error',
      code: 'audit.range_inverted',
      message: 'The end of the range precedes its start.',
    })
  })

  it('surfaces an expired session as unauthorised', async () => {
    refused(401, 'auth.required', 'Sign in again.')
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(log.failure.value).toEqual({ kind: 'unauthorised' })
  })

  it('reports an unreachable service rather than failing silently', async () => {
    get.mockRejectedValue(new Error('offline'))
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(log.failure.value?.kind).toBe('error')
    expect(log.busy.value).toBe(false)
    expect(log.hasMore.value).toBe(false)
  })

  it('drops the page a refused request could not replace', async () => {
    answers()
    const log = useAuditLog(() => 'mine')
    await log.load()
    expect(log.entries.value).toHaveLength(2)

    refused(422, 'audit.range_inverted', 'The end of the range precedes its start.')
    await log.load()

    expect(log.entries.value).toEqual([])
  })

  // AC-9. Absent is not zero, and the composable must not fill either in.
  it('carries an absent row count and elapsed time through untouched', async () => {
    const probe = { ...denied, rowCount: undefined, elapsedMs: undefined }
    get.mockResolvedValue(ok([executed, denied, probe], 50, false))
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(log.entries.value[0]?.rowCount).toBe(12)
    expect(log.entries.value[1]?.rowCount).toBeNull()
    expect(log.entries.value[2]?.rowCount).toBeUndefined()
  })

  // AC-8. Every reason the guard produced, in its order.
  it('carries every reason a denial names', async () => {
    answers()
    const log = useAuditLog(() => 'mine')

    await log.load()

    expect(log.entries.value[1]?.reasons.map((reason) => reason.rule)).toEqual([
      'wildcard_select',
      'column_denied',
    ])
  })

  it('bounds the log by a brushed window exactly, microseconds and all', async () => {
    answers()
    const log = useAuditLog(() => 'all')
    log.from.value = '2025-08-01T00:00'
    log.brushed.value = { from: '2025-08-30T09:00:00.000Z', to: '2025-08-30T11:59:59.999999Z' }

    await log.apply()

    expect(lastQuery()).toMatchObject({
      from: '2025-08-30T09:00:00.000Z',
      to: '2025-08-30T11:59:59.999999Z',
    })
  })

  it('never lets an older answer replace a newer one', async () => {
    let first: (value: unknown) => void = () => undefined
    get.mockImplementationOnce(() => new Promise((resolve) => (first = resolve)))
    get.mockResolvedValueOnce(ok([denied], requestedPageSize, false))
    const log = useAuditLog(() => 'mine')

    const older = log.load()
    await log.load()
    first(ok([executed], requestedPageSize, false))
    await older

    expect(log.entries.value).toEqual([denied])
    expect(log.busy.value).toBe(false)
  })
})
