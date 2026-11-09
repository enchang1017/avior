import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: vi.fn() } }
})

const { useAppliedMigrations, statusTone, statusMeaning } = await import('./useAppliedMigrations')

function row(overrides: Record<string, unknown> = {}) {
  return {
    filename: '001_bet.sql',
    status: 'recorded' as const,
    appliedAt: '2026-09-03T02:00:00Z',
    sql: 'ALTER TABLE public.bet ADD COLUMN settled_at timestamptz;',
    relations: ['public.bet'],
    relationsKnown: true,
    ...overrides,
  }
}

function page(migrations: unknown[]) {
  return {
    data: { database: 'aviordeploy', migrations },
    error: undefined,
    response: { status: 200 },
  }
}

function refusal(status: number, body: unknown) {
  return { data: undefined, error: body, response: { status } }
}

beforeEach(() => get.mockReset())

describe('statuses', () => {
  // Every status the API can send must have a tone and a meaning, or a row
  // arrives on the page with nothing to say about itself.
  it('describes every status the API can send', () => {
    for (const status of ['recorded', 'unaccounted', 'mismatched', 'missing'] as const) {
      expect(statusTone[status]).toBeDefined()
      expect(statusMeaning[status].length).toBeGreaterThan(0)
    }
  })

  // A disagreement between the two ledgers must not read like an ordinary row.
  it('marks every disagreement, and only those', () => {
    expect(statusTone.recorded).toBe('ok')
    for (const status of ['unaccounted', 'mismatched', 'missing'] as const) {
      expect(statusTone[status]).not.toBe('ok')
    }
  })
})

describe('useAppliedMigrations', () => {
  // The endpoint is per database and there is no sensible default: reading the
  // wrong database's history looks exactly like reading the right one.
  it('sends nothing until a database is chosen', async () => {
    const history = useAppliedMigrations()
    await history.load()

    expect(get).not.toHaveBeenCalled()
  })

  it('asks for one database, and for one table when named', async () => {
    get.mockResolvedValue(page([row()]))
    const history = useAppliedMigrations()
    history.database.value = ' aviordeploy '
    history.table.value = '  bet  '
    await history.load()

    expect(get).toHaveBeenLastCalledWith('/databases/{database}/migrations', {
      params: { path: { database: 'aviordeploy' }, query: { table: 'bet' } },
    })
  })

  it('omits the table filter when none is named', async () => {
    get.mockResolvedValue(page([row()]))
    const history = useAppliedMigrations()
    history.database.value = 'aviordeploy'
    await history.load()

    expect(get).toHaveBeenLastCalledWith('/databases/{database}/migrations', {
      params: { path: { database: 'aviordeploy' }, query: {} },
    })
  })

  it('separates the disagreements from the ordinary rows', async () => {
    get.mockResolvedValue(
      page([
        row(),
        row({ filename: 'byhand.sql', status: 'unaccounted', sql: '', relationsKnown: false }),
        row({ filename: '002_bet.sql', status: 'missing', appliedAt: null }),
      ]),
    )
    const history = useAppliedMigrations()
    history.database.value = 'aviordeploy'
    await history.load()

    expect(history.migrations.value).toHaveLength(3)
    expect(history.problems.value.map((m) => m.filename)).toEqual(['byhand.sql', '002_bet.sql'])
  })

  // The one that matters. A database nobody could read is not a database
  // nothing was applied to, and showing an empty list would answer the question
  // wrongly rather than not answering it.
  it('does not show an unreadable database as an empty history', async () => {
    get.mockResolvedValue(page([row()]))
    const history = useAppliedMigrations()
    history.database.value = 'aviordeploy'
    await history.load()
    expect(history.migrations.value).toHaveLength(1)

    get.mockResolvedValue(
      refusal(502, { code: 'release.target_unreadable', message: 'could not be read' }),
    )
    await history.load()

    expect(history.migrations.value).toHaveLength(0)
    expect(history.unreadable.value).toContain('could not be read')
    expect(history.failure.value).toBeNull()
  })

  it('separates a permission refusal from an unreadable target', async () => {
    get.mockResolvedValue(refusal(403, { code: 'x', message: 'no' }))
    const history = useAppliedMigrations()
    history.database.value = 'aviordeploy'
    await history.load()

    expect(history.forbidden.value).toBe(true)
    expect(history.unreadable.value).toBeNull()
  })
})
