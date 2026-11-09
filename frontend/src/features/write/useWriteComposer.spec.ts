import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { POST: post } }
})

const { useWriteComposer, needsColumns, writeKinds } = await import('./useWriteComposer')

function ok(body: unknown) {
  return { data: body, error: undefined, response: { status: 200 } }
}

beforeEach(() => {
  post.mockReset()
})

describe('needsColumns', () => {
  it('asks for columns for everything but a delete, which names none', () => {
    expect(needsColumns('update')).toBe(true)
    expect(needsColumns('insert')).toBe(true)
    expect(needsColumns('delete')).toBe(false)
  })
})

describe('writeKinds', () => {
  it('offers the three statements a write request may be', () => {
    expect(writeKinds.map((kind) => kind.value)).toEqual(['update', 'delete', 'insert'])
  })
})

describe('useWriteComposer', () => {
  it('sends the ticked columns for an update and none for a delete', async () => {
    post.mockResolvedValue(ok({ statement: 'UPDATE public.bet\nSET status = \nWHERE ' }))

    const page = useWriteComposer()
    await page.template('public', 'bet', ['status'])
    expect(post).toHaveBeenCalledWith('/writes/template', {
      body: { kind: 'update', schema: 'public', table: 'bet', columns: ['status'] },
    })

    post.mockClear()
    page.kind.value = 'delete'
    await page.template('public', 'bet', ['status'])
    expect(post).toHaveBeenCalledWith('/writes/template', {
      body: { kind: 'delete', schema: 'public', table: 'bet' },
    })
  })

  /*
   * A verdict shown beside a statement it was not about is worse than no
   * verdict: it is an answer to a question nobody asked, and the row count
   * beside it is how many rows some other statement would have touched.
   */
  it('drops the verdict the moment the statement changes', async () => {
    post.mockResolvedValue(ok({ decision: 'allow', reasons: [], estimatedRows: 3 }))

    const page = useWriteComposer()
    page.statement.value = 'DELETE FROM public.bet WHERE bet_id = 1'
    await page.check('aviorfixture')
    expect(page.allowed.value).toBe(true)

    page.edited()
    expect(page.verdict.value).toBeNull()
    expect(page.checked.value).toBe(false)
    expect(page.allowed.value).toBe(false)
  })

  it('inserting a template also drops the verdict it invalidated', async () => {
    post.mockResolvedValueOnce(ok({ decision: 'allow', reasons: [] }))
    const page = useWriteComposer()
    await page.check('aviorfixture')
    expect(page.checked.value).toBe(true)

    post.mockResolvedValueOnce(ok({ statement: 'DELETE FROM public.bet\nWHERE ' }))
    await page.template('public', 'bet', [])
    expect(page.statement.value).toContain('DELETE FROM public.bet')
    expect(page.checked.value).toBe(false)
  })

  // A refusal is the guard answering, so the reasons are what the composer
  // renders. It is not a failure of the page and must not read as one.
  it('keeps a deny as a verdict rather than as a failure', async () => {
    post.mockResolvedValue(
      ok({
        decision: 'deny',
        reasons: [{ rule: 'missing_where_clause', message: 'UPDATE must carry a WHERE clause' }],
      }),
    )

    const page = useWriteComposer()
    await page.check('aviorfixture')

    expect(page.failure.value).toBeNull()
    expect(page.allowed.value).toBe(false)
    expect(page.verdict.value?.reasons.at(0)?.rule).toBe('missing_where_clause')
  })

  /*
   * `"x"` is an identifier in PostgreSQL, and somebody meaning a string writes
   * it every day. Reading the server's refusal as a fault sends them to whoever
   * runs Avior with a question about their own typing.
   */
  it('keeps a statement the server refused apart from a failure of the page', async () => {
    post.mockResolvedValue({
      data: undefined,
      error: {
        code: 'write.statement_invalid',
        message: 'The server refused to compile the statement: column "adsf" does not exist.',
      },
      response: { status: 422 },
    })

    const page = useWriteComposer()
    page.statement.value = 'UPDATE public.awkward SET "MiXeD" = "adsf" WHERE id = 2'
    await page.check('aviorfixture')

    expect(page.failure.value).toBeNull()
    expect(page.refusal.value?.code).toBe('write.statement_invalid')
    expect(page.refusal.value?.message).toContain('does not exist')

    // And it goes when the statement does, like every other answer about it.
    page.edited()
    expect(page.refusal.value).toBeNull()
  })

  it('carries what would be kept, so the claim can be read as facts', async () => {
    post.mockResolvedValue(
      ok({
        decision: 'allow',
        reasons: [],
        targetTable: 'public.awkward',
        estimatedRows: 1,
        backup: {
          table: 'avior_backup.captured_row',
          direction: 'before',
          rows: 1,
          reverseStatement: 'UPDATE public.awkward AS t ...',
        },
      }),
    )

    const page = useWriteComposer()
    await page.check('aviorfixture')

    expect(page.allowed.value).toBe(true)
    expect(page.verdict.value?.backup?.direction).toBe('before')
    expect(page.verdict.value?.backup?.table).toBe('avior_backup.captured_row')
  })

  it('reports an unreachable server rather than throwing at the view', async () => {
    post.mockRejectedValue(new TypeError('failed to fetch'))

    const page = useWriteComposer()
    await page.check('aviorfixture')

    expect(page.failure.value).not.toBeNull()
    expect(page.busy.value).toBe(false)
  })
})
