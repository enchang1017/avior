import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'
import type { ColumnRef, QueryResult, TableRef } from '@/shared/api'

const post = vi.fn()
vi.mock('@/shared/api', async (original) => ({
  ...(await original<typeof sharedApi>()),
  api: { POST: post },
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
beforeEach(() => post.mockReset())

function stage() {
  const edits = useRowEdits()
  edits.stage('db', table, columns, result, 0, 'note', 'new')
  return edits
}
async function review(edits: ReturnType<typeof useRowEdits>) {
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
    ).toContain('type')
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
    expect(post.mock.calls[0]?.[1]).toEqual({
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
