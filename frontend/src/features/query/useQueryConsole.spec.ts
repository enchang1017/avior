import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { POST: post, GET: vi.fn() } }
})

const { useQueryConsole } = await import('./useQueryConsole')

const target = {
  instanceId: '00000000-0000-0000-0000-0000000000a1',
  database: 'aviorfixture',
  statement: 'SELECT member_id FROM public.member',
}

const allowVerdict = {
  decision: 'allow' as const,
  reasons: [],
  referencedTables: ['public.member'],
  referencedColumns: ['public.member.member_id'],
}

const denyVerdict = {
  decision: 'deny' as const,
  reasons: [
    { rule: 'wildcard_select' as const, message: 'SELECT * is not permitted' },
    { rule: 'cost_limit_exceeded' as const, message: 'too expensive' },
  ],
  referencedTables: [],
  referencedColumns: [],
}

describe('useQueryConsole', () => {
  beforeEach(() => post.mockReset())

  it('stores the verdict returned by inspect', async () => {
    post.mockResolvedValue({ data: allowVerdict, response: { status: 200 } })
    const console = useQueryConsole()

    await console.inspect(target)

    expect(console.verdict.value).toEqual(allowVerdict)
    expect(console.result.value).toBeNull()
    expect(console.failure.value).toBeNull()
    expect(console.busy.value).toBe(false)
  })

  it('stores the result returned by execute', async () => {
    const result = {
      columns: [{ name: 'member_id', type: 'integer' }],
      rows: [[1]],
      rowCount: 1,
      truncated: false,
      elapsedMs: 4,
      auditId: '00000000-0000-0000-0000-0000000000ff',
    }
    post.mockResolvedValue({ data: result, response: { status: 200 } })
    const console = useQueryConsole()

    await console.execute(target)

    expect(console.result.value).toEqual(result)
    expect(console.failure.value).toBeNull()
  })

  // A refusal has to surface both the message and the rules. Showing only the
  // failure text would leave the operator without the reasons.
  it('surfaces the verdict as well as the failure when execute is refused', async () => {
    post.mockResolvedValue({ data: undefined, error: denyVerdict, response: { status: 403 } })
    const console = useQueryConsole()

    await console.execute(target)

    expect(console.failure.value).toEqual({ kind: 'rejected', verdict: denyVerdict })
    expect(console.verdict.value).toEqual(denyVerdict)
    expect(console.result.value).toBeNull()
  })

  it('clears the previous outcome before a new request', async () => {
    post.mockResolvedValueOnce({ data: allowVerdict, response: { status: 200 } })
    const console = useQueryConsole()
    await console.inspect(target)
    expect(console.verdict.value).not.toBeNull()

    post.mockResolvedValueOnce({
      data: undefined,
      error: { code: 'internal', message: 'boom' },
      response: { status: 500 },
    })
    await console.inspect(target)

    expect(console.verdict.value).toBeNull()
    expect(console.failure.value).toEqual({ kind: 'error', code: 'internal', message: 'boom' })
  })

  it('keeps two consoles independent', async () => {
    post.mockResolvedValue({ data: allowVerdict, response: { status: 200 } })
    const first = useQueryConsole()
    const second = useQueryConsole()

    await first.inspect(target)

    expect(first.verdict.value).not.toBeNull()
    expect(second.verdict.value).toBeNull()
  })

  // FEAT-074 AC-6: the pane says which answer it is waiting for.
  it('says which request is running until it answers', async () => {
    let answer: (value: unknown) => void = () => undefined
    post.mockReturnValue(new Promise((resolve) => (answer = resolve)))
    const console = useQueryConsole()

    const executing = console.execute(target)
    expect(console.running.value).toBe('execute')
    answer({ data: allowVerdict, response: { status: 200 } })
    await executing
    expect(console.running.value).toBeNull()

    post.mockReturnValue(new Promise((resolve) => (answer = resolve)))
    const inspecting = console.inspect(target)
    expect(console.running.value).toBe('inspect')
    answer({ data: allowVerdict, response: { status: 200 } })
    await inspecting
    expect(console.running.value).toBeNull()
  })
})
