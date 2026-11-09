import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { useSchemaBrowser } = await import('./useSchemaBrowser')

const instanceId = '00000000-0000-0000-0000-0000000000a1'

const memberTable = { schema: 'public', name: 'member', kind: 'table' as const }
const memberView = { schema: 'public', name: 'member_view', kind: 'view' as const }
const tables = [memberTable, memberView]

const columns = [
  { name: 'member_id', type: 'integer', nullable: false, denied: false },
  { name: 'login_name', type: 'text', nullable: false, denied: false },
  { name: 'id_card_number', type: 'text', nullable: false, denied: true },
  { name: 'register_time', type: 'timestamp with time zone', nullable: false, denied: false },
  { name: 'last_seen', type: 'date', nullable: true, denied: false },
]

function ok<T>(data: T) {
  return { data, response: { status: 200 } }
}

/** route answers by path so a test does not depend on call order. */
function routeGet(answers: Record<string, unknown>) {
  get.mockImplementation((path: string) => {
    if (path in answers) return Promise.resolve(ok(answers[path]))
    return Promise.resolve({ data: undefined, error: null, response: { status: 404 } })
  })
}

async function browseToColumns() {
  routeGet({
    '/schema/databases': ['aviorfixture'],
    '/schema/tables': tables,
    '/schema/columns': columns,
  })
  const browser = useSchemaBrowser(instanceId)
  await browser.loadDatabases()
  await browser.chooseDatabase('aviorfixture')
  await browser.chooseTable(memberTable)
  return browser
}

describe('useSchemaBrowser', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('loads the database list', async () => {
    routeGet({ '/schema/databases': ['aviorfixture', 'AccountingDB'] })
    const browser = useSchemaBrowser(instanceId)

    await browser.loadDatabases()

    expect(browser.databases.value).toEqual(['aviorfixture', 'AccountingDB'])
    expect(browser.failure.value).toBeNull()
    expect(browser.busy.value).toBe(false)
  })

  it('loads tables when a database is chosen', async () => {
    routeGet({ '/schema/databases': ['aviorfixture'], '/schema/tables': tables })
    const browser = useSchemaBrowser(instanceId)

    await browser.chooseDatabase('aviorfixture')

    expect(browser.database.value).toBe('aviorfixture')
    expect(browser.tables.value).toEqual(tables)
  })

  it('selects every readable column when a table is chosen', async () => {
    const browser = await browseToColumns()

    expect(browser.columns.value).toHaveLength(columns.length)
    expect([...browser.selected.value].sort()).toEqual([
      'last_seen',
      'login_name',
      'member_id',
      'register_time',
    ])
    expect(browser.allSelected.value).toBe(true)
  })

  // A stale column list after the table changed is how an operator builds a
  // statement over the wrong object, and the names look plausible enough not to
  // notice.
  it('clears everything downstream when the database changes', async () => {
    const browser = await browseToColumns()
    expect(browser.columns.value).not.toHaveLength(0)

    routeGet({ '/schema/tables': [] })
    await browser.chooseDatabase('Other')

    expect(browser.table.value).toBeNull()
    expect(browser.columns.value).toEqual([])
    expect(browser.selected.value.size).toBe(0)
  })

  it('clears the columns when the table is cleared', async () => {
    const browser = await browseToColumns()

    await browser.chooseTable(null)

    expect(browser.columns.value).toEqual([])
    expect(browser.selected.value.size).toBe(0)
  })

  it('does not load columns without a database', async () => {
    routeGet({})
    const browser = useSchemaBrowser(instanceId)

    await browser.chooseTable(memberTable)

    expect(browser.columns.value).toEqual([])
  })

  it('toggles a readable column', async () => {
    const browser = await browseToColumns()

    browser.toggle('member_id')
    expect(browser.selected.value.has('member_id')).toBe(false)
    expect(browser.allSelected.value).toBe(false)

    browser.toggle('member_id')
    expect(browser.selected.value.has('member_id')).toBe(true)
  })

  // Selecting it would build a statement certain to be refused.
  it('refuses to select a denied column', async () => {
    const browser = await browseToColumns()

    browser.toggle('id_card_number')

    expect(browser.selected.value.has('id_card_number')).toBe(false)
  })

  it('ignores a column the table does not have', async () => {
    const browser = await browseToColumns()
    const before = browser.selected.value.size

    browser.toggle('NoSuchColumn')

    expect(browser.selected.value.size).toBe(before)
  })

  it('toggles all readable columns and never the denied one', async () => {
    const browser = await browseToColumns()

    browser.toggleAll()
    expect(browser.selected.value.size).toBe(0)

    browser.toggleAll()
    expect([...browser.selected.value].sort()).toEqual([
      'last_seen',
      'login_name',
      'member_id',
      'register_time',
    ])
    expect(browser.selected.value.has('id_card_number')).toBe(false)
  })

  it('asks the server to build the statement', async () => {
    const browser = await browseToColumns()
    post.mockResolvedValue(
      ok({ statement: 'SELECT "member_id", "login_name" FROM "public"."member" LIMIT 100' }),
    )

    const statement = await browser.buildStatement(100)

    expect(statement).toContain('LIMIT 100')
    expect(post).toHaveBeenCalledWith('/query/template', {
      body: {
        instanceId,
        database: 'aviorfixture',
        schema: 'public',
        table: 'member',
        columns: ['member_id', 'login_name', 'register_time', 'last_seen'],
        top: 100,
      },
    })
  })

  it('does not build a statement with nothing selected', async () => {
    const browser = await browseToColumns()
    browser.toggleAll()

    await expect(browser.buildStatement(100)).resolves.toBeNull()
    expect(post).not.toHaveBeenCalled()
  })

  it('does not build a statement without a table', async () => {
    routeGet({ '/schema/databases': ['aviorfixture'] })
    const browser = useSchemaBrowser(instanceId)
    await browser.loadDatabases()

    await expect(browser.buildStatement(100)).resolves.toBeNull()
    expect(post).not.toHaveBeenCalled()
  })

  it('surfaces a rejected build', async () => {
    const browser = await browseToColumns()
    post.mockResolvedValue({
      data: undefined,
      error: {
        code: 'schema.column_denied',
        message: 'You are not permitted to read id_card_number.',
      },
      response: { status: 422 },
    })

    await expect(browser.buildStatement(100)).resolves.toBeNull()
    expect(browser.failure.value).toEqual({
      kind: 'error',
      code: 'schema.column_denied',
      message: 'You are not permitted to read id_card_number.',
    })
  })

  it('surfaces a request that never reached the server', async () => {
    get.mockImplementation(() => Promise.reject(new Error('network down')))
    const browser = useSchemaBrowser(instanceId)

    await browser.loadDatabases()

    const failure = browser.failure.value
    expect(failure?.kind).toBe('error')
    expect(failure).toMatchObject({ code: 'network.unreachable' })
    expect(browser.busy.value).toBe(false)
  })

  describe('ordering', () => {
    /*
     * A timestamp is what an operator almost always means by "order by": they
     * want the newest rows. Everything else stays on the list — sorting by an
     * amount or a name is ordinary — but the dates go first rather than being
     * hunted for among forty columns.
     */
    it('offers the dates before everything else', async () => {
      const browser = await browseToColumns()

      expect(browser.orderableColumns.value).toEqual([
        'register_time',
        'last_seen',
        'member_id',
        'login_name',
      ])
    })

    // ORDER BY reads the column, so the server refuses a denied one. Offering
    // it would be suggesting a statement that cannot run.
    it('offers no column the caller may not read', async () => {
      const browser = await browseToColumns()

      expect(browser.orderableColumns.value).not.toContain('id_card_number')
      expect(browser.temporalColumns.value).not.toContain('id_card_number')
    })

    it('knows which columns are dates', async () => {
      const browser = await browseToColumns()

      expect(browser.temporalColumns.value).toEqual(['register_time', 'last_seen'])
    })

    it('asks the server to order, and which way', async () => {
      const browser = await browseToColumns()
      post.mockResolvedValue(ok({ statement: 'SELECT 1' }))

      browser.orderBy.value = 'register_time'
      browser.descending.value = true
      await browser.buildStatement(100)

      expect(post).toHaveBeenLastCalledWith('/query/template', {
        body: expect.objectContaining({ orderBy: 'register_time', descending: true }),
      })
    })

    // No ordering is the ordinary case, and a statement that says nothing about
    // order is more honest than one pretending the rows arrive sorted.
    it('says nothing about ordering when none is chosen', async () => {
      const browser = await browseToColumns()
      post.mockResolvedValue(ok({ statement: 'SELECT 1' }))

      await browser.buildStatement(100)

      const body = post.mock.calls.at(-1)?.[1].body
      expect(body).not.toHaveProperty('orderBy')
      expect(body).not.toHaveProperty('descending')
    })

    // A new table has different columns, so an ordering carried over from the
    // last one would name a column this table does not have.
    it('forgets the ordering when the table changes', async () => {
      const browser = await browseToColumns()
      browser.orderBy.value = 'register_time'

      await browser.chooseTable(memberView)

      expect(browser.orderBy.value).toBe('')
    })
  })
})
