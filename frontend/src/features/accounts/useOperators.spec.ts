import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const put = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, PUT: put, POST: vi.fn() } }
})

const { useOperators, fallbackRoles, holdsRole } = await import('./useOperators')

function operator(overrides: Record<string, unknown> = {}) {
  return {
    id: '00000000-0000-0000-0000-000000000001',
    username: 'developer',
    kind: 'person' as const,
    roles: ['developer'],
    disabled: false,
    createdAt: '2026-09-01T00:00:00Z',
    ...overrides,
  }
}

function ok(body: unknown) {
  return { data: body, error: undefined, response: { status: 200 } }
}

function refusal(status: number, body: unknown) {
  return { data: undefined, error: body, response: { status } }
}

beforeEach(() => {
  get.mockReset()
  put.mockReset()
})

describe('the roles this page offers', () => {
  // The administrator role is refused by the server whoever asks, so drawing a
  // column for it would be a control that never works.
  it('does not fall back to a column for administrator', () => {
    expect(fallbackRoles).not.toContain('administrator')
  })

  // What a role opens is the server's answer, built from the table the
  // middleware reads. A copy here would go on describing a role by what it used
  // to carry, and nothing would fail.
  it('keeps only the roles the server marked administrable', async () => {
    get.mockResolvedValue(
      ok([
        {
          name: 'developer',
          label: 'Developer',
          summary: 'Runs statements.',
          administrable: true,
          permissions: [],
        },
        {
          name: 'administrator',
          label: 'Administrator',
          summary: 'Runs the gateway.',
          administrable: false,
          permissions: [],
        },
      ]),
    )
    const page = useOperators()

    await page.loadRoles()

    expect(page.roles.value).toHaveLength(2)
    expect(page.administrable.value.map((role) => role.name)).toEqual(['developer'])
  })

  // Being unable to explain a role is not a reason to stop somebody granting
  // one, so the read fails quietly and the columns fall back.
  it('raises no banner when the role model cannot be read', async () => {
    get.mockRejectedValue(new Error('offline'))
    const page = useOperators()

    await page.loadRoles()

    expect(page.roles.value).toEqual([])
    expect(page.failure.value).toBeNull()
  })
})

describe('useOperators', () => {
  it('lists what the server sent', async () => {
    get.mockResolvedValue(ok([operator()]))
    const page = useOperators()

    await page.load()

    expect(page.operators.value).toHaveLength(1)
    expect(page.loaded.value).toBe(true)
    expect(page.failure.value).toBeNull()
  })

  // An empty list would read as "nobody is provisioned", which is a different
  // and much more alarming answer than "you may not read this".
  it('shows no operators and a reason when the read is refused', async () => {
    get.mockResolvedValue(refusal(403, { code: 'permission.denied', message: 'Not for you.' }))
    const page = useOperators()

    await page.load()

    expect(page.operators.value).toEqual([])
    expect(page.loaded.value).toBe(false)
    expect(page.failure.value).not.toBeNull()
  })

  it('reports a request that never arrived', async () => {
    get.mockRejectedValue(new Error('offline'))
    const page = useOperators()

    await page.load()

    expect(page.failure.value).toEqual(
      expect.objectContaining({ kind: 'error', code: 'network.unreachable' }),
    )
  })

  it('filters by name, on what is typed', async () => {
    get.mockResolvedValue(
      ok([
        operator({ id: 'a', username: 'alice' }),
        operator({ id: 'b', username: 'bob' }),
        operator({ id: 'c', username: 'roberta' }),
      ]),
    )
    const page = useOperators()
    await page.load()

    page.search.value = 'ob'

    expect(page.shown.value.map((found) => found.username)).toEqual(['bob', 'roberta'])
  })

  it('sends the opposite of what the operator holds', async () => {
    const held = operator({ roles: ['developer'] })
    put.mockResolvedValue(ok(operator({ roles: [] })))
    const page = useOperators()
    get.mockResolvedValue(ok([held]))
    await page.load()

    await page.setRole(held, 'developer', !holdsRole(held, 'developer'))

    expect(put).toHaveBeenCalledWith(
      '/operators/{operatorId}/roles/{role}',
      expect.objectContaining({ body: { granted: false } }),
    )
  })

  /*
   * The response is the authority, not the request replayed onto the row.
   *
   * Granting a role somebody already holds is not an error and changes nothing,
   * so the server answers with the operator unchanged. A row edited
   * optimistically would show a change the server did not make.
   */
  it('takes the row the server sent back rather than assuming the change', async () => {
    const subject = operator({ roles: [] })
    get.mockResolvedValue(ok([subject]))
    put.mockResolvedValue(ok(operator({ roles: [] })))
    const page = useOperators()
    await page.load()

    await page.setRole(subject, 'dba', true)

    expect(page.operators.value[0]?.roles).toEqual([])
  })

  // A refusal has to reach the page, and it must not leave the row claiming a
  // role the server declined to grant.
  it('keeps the row as it was when the change is refused', async () => {
    const subject = operator({ roles: [] })
    get.mockResolvedValue(ok([subject]))
    put.mockResolvedValue(
      refusal(403, {
        code: 'accounts.role_not_administrable',
        message: 'The administrator role is not granted here.',
      }),
    )
    const page = useOperators()
    await page.load()

    await page.setRole(subject, 'administrator', true)

    expect(page.operators.value[0]?.roles).toEqual([])
    expect(page.failure.value).toEqual(
      expect.objectContaining({ message: 'The administrator role is not granted here.' }),
    )
  })

  // One row at a time, so a change in flight disables its own switches and not
  // the whole page.
  it('names the row being changed, and stops naming it when the change lands', async () => {
    const subject = operator()
    get.mockResolvedValue(ok([subject]))
    put.mockResolvedValue(ok(operator({ disabled: true })))
    const page = useOperators()
    await page.load()

    const inFlight = page.setDisabled(subject, true)
    expect(page.changing.value).toBe(subject.id)
    await inFlight

    expect(page.changing.value).toBe('')
    expect(page.operators.value[0]?.disabled).toBe(true)
  })

  it('stops naming the row being changed when the change fails', async () => {
    const subject = operator()
    get.mockResolvedValue(ok([subject]))
    put.mockRejectedValue(new Error('offline'))
    const page = useOperators()
    await page.load()

    await page.setDisabled(subject, true)

    expect(page.changing.value).toBe('')
    expect(page.failure.value).not.toBeNull()
  })
})
