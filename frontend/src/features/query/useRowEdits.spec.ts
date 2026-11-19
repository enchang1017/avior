import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'
import type { ColumnRef, QueryResult, TableRef } from '@/shared/api'

const post = vi.fn()
const get = vi.fn()
vi.mock('@/shared/api', async (original) => ({
  ...(await original<typeof sharedApi>()),
  api: { POST: post, GET: get },
}))
const { useRowEdits, editUnavailable, supportedCellType } = await import('./useRowEdits')
const table: TableRef = { schema: 'public', name: 'items', kind: 'table' }
const keyColumn: ColumnRef = {
  name: 'id',
  type: 'bigint',
  nullable: false,
  denied: false,
  primaryKeyPosition: 1,
}
const valueColumn: ColumnRef = { name: 'note', type: 'text', nullable: true, denied: false }
const columns: ColumnRef[] = [keyColumn, valueColumn]
const result: QueryResult = {
  columns: [
    { name: 'id', type: 'int8' },
    { name: 'note', type: 'text' },
  ],
  rows: [
    ['9007199254740993', 'old'],
    ['2', null],
  ],
  rowCount: 2,
  elapsedMs: 1,
  auditId: 'test',
  limits: { maxCost: 50, maxRows: 100 },
  truncated: false,
}
function ok(data: unknown) {
  return { data, response: { status: 200 } }
}
function reject(status: number) {
  return { error: { code: 'test.denied', message: 'not allowed' }, response: { status } }
}
beforeEach(() => {
  post.mockReset()
  get.mockReset()
})

function stage() {
  const edits = useRowEdits()
  edits.stage('db', table, columns, result, 0, 'note', 'new')
  return edits
}
function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error('Missing test fixture')
  return value
}
function current(rows = [required(result.rows[0])]) {
  post
    .mockResolvedValueOnce(ok({ statement: 'SELECT current' }))
    .mockResolvedValueOnce(ok({ ...result, rows }))
}
async function review(edits: ReturnType<typeof useRowEdits>) {
  current()
  post
    .mockResolvedValueOnce(ok({ statement: 'UPDATE public.items SET note=1 WHERE id=1' }))
    .mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
  return edits.review()
}

describe('staged row edits', () => {
  it('preserves the original row, overlays by primary key across pages and removes reverted changes', () => {
    const edits = stage()
    expect(edits.count.value).toBe(1)
    expect(edits.drafts.value[0]?.original).toEqual({ id: '9007199254740993', note: 'old' })
    expect(
      edits.overlay('db', table, columns, { ...result, rows: [...result.rows].reverse() }),
    ).toEqual({ '1:note': 'new' })
    expect(edits.overlay('other-db', table, columns, result)).toEqual({})
    edits.stage('db', table, columns, result, 0, 'note', 'old')
    expect(edits.count.value).toBe(0)
  })
  it('distinguishes empty strings from NULL and keeps drafts for other tables', () => {
    const edits = stage()
    edits.stage('db', table, columns, result, 0, 'note', '')
    edits.stage('db', table, columns, result, 1, 'note', '')
    edits.stage('other-db', table, columns, result, 0, 'note', null)
    expect(edits.drafts.value).toHaveLength(3)
    edits.discardRow(edits.drafts.value[0]?.id ?? '')
    expect(edits.drafts.value).toHaveLength(2)
    edits.discard()
    expect(edits.count.value).toBe(0)
  })
  it('refuses views, missing keys, unsupported types, masked and partial results', () => {
    expect(editUnavailable({ ...table, kind: 'view' }, columns, result)).toContain('ordinary')
    expect(
      editUnavailable(
        table,
        columns.map((field) => ({ ...field, primaryKeyPosition: 0 })),
        result,
      ),
    ).toContain('primary key')
    expect(
      editUnavailable(table, [{ ...keyColumn, type: 'bytea' }, valueColumn], result),
    ).toContain('Unsupported')
    expect(
      editUnavailable(table, columns, {
        ...result,
        columns: [{ name: 'id', type: 'int8', masked: true }],
      }),
    ).toContain('Restricted')
    expect(
      editUnavailable(table, columns, { ...result, columns: result.columns.slice(0, 1) }),
    ).toContain('all columns')
    const edits = useRowEdits()
    edits.stage('db', { ...table, kind: 'view' }, columns, result, 0, 'note', 'x')
    edits.stage('db', table, columns, result, 0, 'id', '3')
    edits.stage('db', table, columns, result, 0, 'unknown', 'x')
    edits.stage(
      'db',
      table,
      columns.map((field) => ({ ...field, generated: true })),
      result,
      0,
      'note',
      'x',
    )
    expect(edits.count.value).toBe(0)
  })
  it('checks every statement before submitting with the one-row expectation', async () => {
    const edits = stage()
    await edits.submit('why')
    expect(post).not.toHaveBeenCalled()
    expect(await review(edits)).toBe(true)
    expect(post.mock.calls[2]?.[1]).toMatchObject({
      body: {
        database: 'db',
        schema: 'public',
        table: 'items',
        original: { id: '9007199254740993', note: 'old' },
        changes: { note: 'new' },
      },
    })
    post.mockResolvedValueOnce(ok({ id: 'request', state: 'applied' }))
    await edits.submit('why')
    expect(post.mock.lastCall?.[1]).toMatchObject({
      body: { expectedAffectedRows: 1, reason: 'why' },
    })
    expect(edits.count.value).toBe(0)
    expect(edits.message.value).toContain('1 saved')
  })
  it('removes submitted production drafts and retains conflicted rows', async () => {
    const edits = stage()
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'request', state: 'failed', error: 'write conflict' }))
    await edits.submit('why')
    expect(edits.drafts.value[0]?.error).toBe('write conflict')
    expect(edits.count.value).toBe(1)
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'pending', state: 'pending' }))
    await edits.submit('why')
    expect(edits.count.value).toBe(0)
    expect(edits.message.value).toContain('1 awaiting approval')
  })
  it('retains edits after transport, policy, template or inspection failures', async () => {
    const edits = stage()
    post.mockResolvedValueOnce(reject(403))
    expect(await edits.review()).toBe(false)
    expect(edits.drafts.value[0]?.error).toContain('not allowed')
    current()
    post
      .mockResolvedValueOnce(ok({ statement: 'UPDATE' }))
      .mockResolvedValueOnce(ok({ decision: 'deny', reasons: [{ message: 'denied column' }] }))
    expect(await edits.review()).toBe(false)
    expect(edits.drafts.value[0]?.error).toContain('denied column')
    post.mockRejectedValueOnce(new Error('network'))
    expect(await edits.review()).toBe(false)
    await review(edits)
    post.mockRejectedValueOnce(new Error('network'))
    await edits.submit('why')
    expect(edits.count.value).toBe(1)
    await review(edits)
    post.mockResolvedValueOnce(reject(422))
    await edits.submit('why')
    expect(edits.count.value).toBe(1)
  })
  it('keeps the same supported type boundary as the server', () => {
    for (const type of ['text[]', 'jsonb[]', 'timestamp(6) with time zone', 'numeric(18,4)'])
      expect(supportedCellType(type)).toBe(true)
    for (const type of ['json[]', 'bytea', 'custom_enum'])
      expect(supportedCellType(type)).toBe(false)
  })
})

describe('conflicts and request tracking', () => {
  it('requires explicit choices for conflicting fields, adopts the full latest snapshot and checks again', async () => {
    const edits = stage()
    current([['9007199254740993', 'other']])
    expect(await edits.review()).toBe(false)
    const row = required(edits.drafts.value[0])
    expect(row.latest?.note).toBe('other')
    expect(edits.rebase(row)).toBe(false)
    row.choices.note = 'mine'
    expect(edits.rebase(row)).toBe(true)
    expect(row.original.note).toBe('other')
    expect(row.changes.note).toBe('new')
    expect(edits.checked.value).toBe(false)
    current([['9007199254740993', 'other']])
    post
      .mockResolvedValueOnce(ok({ statement: 'UPDATE exact latest' }))
      .mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
    expect(await edits.review()).toBe(true)
    expect(edits.readyRows.value).toHaveLength(1)
  })
  it('lets a current-value resolution discard an edit without writing', async () => {
    const edits = stage()
    current([['9007199254740993', 'other']])
    await edits.review()
    const row = required(edits.drafts.value[0])
    row.choices.note = 'current'
    expect(edits.rebase(row)).toBe(true)
    expect(edits.count.value).toBe(0)
    expect(post.mock.calls.map((call) => call[0])).not.toContain('/writes')
  })
  it('checks later rows when one was deleted and submits only ready rows', async () => {
    const edits = stage()
    edits.stage('db', table, columns, result, 1, 'note', 'second')
    current([])
    current([required(result.rows[1])])
    post
      .mockResolvedValueOnce(ok({ statement: 'UPDATE second' }))
      .mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
    expect(await edits.review()).toBe(false)
    expect(edits.drafts.value[0]?.latest).toBeNull()
    expect(edits.readyRows.value).toHaveLength(1)
    post.mockResolvedValueOnce(ok({ id: 'second', state: 'pending' }))
    await edits.submit('fix')
    expect(edits.drafts.value).toHaveLength(1)
    expect(edits.outcomes.value[0]?.row.changes.note).toBe('second')
  })
  it('retains pending drafts, blocks duplicate row edits and restores a rejected request', async () => {
    const edits = stage()
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'pending', state: 'pending' }))
    await edits.submit('why')
    expect(edits.blockedRows('db', table, columns, result)[0]).toContain('approval')
    edits.stage('db', table, columns, result, 0, 'note', 'duplicate')
    expect(edits.count.value).toBe(0)
    get.mockResolvedValueOnce(ok({ request: { state: 'rejected', decisionNote: 'Need context' } }))
    await edits.refreshOutcomes()
    expect(edits.drafts.value[0]?.changes.note).toBe('new')
    expect(edits.drafts.value[0]?.error).toBe('Need context')
    expect(edits.outcomes.value).toHaveLength(1)
    get.mockResolvedValueOnce(ok({ request: { state: 'rejected' } }))
    await edits.refreshOutcomes()
    expect(edits.drafts.value).toHaveLength(1)
  })
  it('refreshes applied and undone status and retains history after discarding local drafts', async () => {
    const edits = stage()
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'pending', state: 'pending' }))
    await edits.submit('why')
    get.mockResolvedValueOnce(ok({ request: { state: 'applied' } }))
    await edits.refreshOutcomes()
    expect(edits.appliedVersion.value).toBe(1)
    get.mockResolvedValueOnce(ok({ request: { state: 'applied', rolledBackBy: 'undo' } }))
    await edits.refreshOutcomes()
    expect(edits.outcomes.value[0]?.state).toBe('undone')
    expect(edits.appliedVersion.value).toBe(2)
    edits.discard()
    expect(edits.outcomes.value).toHaveLength(1)
    edits.reset()
    expect(edits.outcomes.value).toHaveLength(0)
  })
  it('does not overwrite a newer local draft while restoring an older request', async () => {
    const edits = stage()
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'old', state: 'applied' }))
    await edits.submit('why')
    edits.stage('db', table, columns, result, 0, 'note', 'newer')
    edits.restore(required(edits.outcomes.value[0]))
    expect(edits.drafts.value[0]?.changes.note).toBe('newer')
    expect(edits.outcomes.value[0]?.notice).toContain('newer')
  })
  it('aborts a lookup after reset and never executes its resulting SELECT', async () => {
    const edits = stage()
    let resolve!: (value: unknown) => void
    post.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    const pending = edits.review()
    const signal = post.mock.calls[0]?.[1].signal as AbortSignal
    edits.reset()
    expect(signal.aborted).toBe(true)
    resolve(ok({ statement: 'SELECT old user' }))
    await pending
    expect(post).toHaveBeenCalledTimes(1)
    expect(edits.checked.value).toBe(false)
    expect(edits.drafts.value).toHaveLength(0)
  })
  it('does not refill outcomes or submit a second row after session reset', async () => {
    const edits = stage()
    await review(edits)
    edits.stage('db', table, columns, result, 1, 'note', 'second')
    current()
    post
      .mockResolvedValueOnce(ok({ statement: 'UPDATE first' }))
      .mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
    current([required(result.rows[1])])
    post
      .mockResolvedValueOnce(ok({ statement: 'UPDATE second' }))
      .mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
    await edits.review()
    let resolve!: (value: unknown) => void
    post.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    const pending = edits.submit('why')
    const calls = post.mock.calls.length
    edits.reset()
    resolve(ok({ id: 'stale', state: 'applied' }))
    await pending
    expect(post).toHaveBeenCalledTimes(calls)
    expect(edits.outcomes.value).toHaveLength(0)
  })
  it('does not restore old user data after an in-flight status refresh completes', async () => {
    const edits = stage()
    await review(edits)
    post.mockResolvedValueOnce(ok({ id: 'pending', state: 'pending' }))
    await edits.submit('why')
    let resolve!: (value: unknown) => void
    get.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    const pending = edits.refreshOutcomes()
    edits.reset()
    resolve(ok({ request: { state: 'failed', error: 'conflict' } }))
    await pending
    expect(edits.outcomes.value).toHaveLength(0)
    expect(edits.drafts.value).toHaveLength(0)
    expect(edits.refreshing.value).toBe(false)
  })
})
