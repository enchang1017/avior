import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { useProvisioning, nameProblem, describeWriteGaps } = await import('./useProvisioning')

function report(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      database: 'shop',
      databaseExists: true,
      ownerRole: 'shop_schema_owner',
      ownerRoleExists: true,
      deployerOwnsSchema: true,
      roles: [],
      ...overrides,
    },
    error: undefined,
    response: { status: 200 },
  }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
})

/*
 * The name check is here as well as at the server, and the two have to agree:
 * a page that refused what the server accepts is a page nobody can use, and one
 * that accepted what the server refuses sends a round trip to find out.
 */
describe('nameProblem', () => {
  it.each(['shop', 'a', 'role_shop', 'shop_pf_2', 'a'.repeat(63)])('accepts %p', (name) => {
    expect(nameProblem(name)).toBeNull()
  })

  it.each([
    ['Shop', 'an upper-case letter'],
    ['1shop', 'a leading digit'],
    ['shop-pf', 'a hyphen'],
    ['shop pf', 'a space'],
    ['a'.repeat(64), 'one character past the limit'],
    ['shop"; DROP DATABASE aviorfixture; --', 'an injection'],
  ])('refuses %p, which is %s', (name) => {
    expect(nameProblem(name)).not.toBeNull()
  })

  it('asks for a name rather than complaining about the empty one', () => {
    expect(nameProblem('   ')).toBe('Name the database.')
  })
})

describe('useProvisioning', () => {
  it('refuses a name that is not an identifier without asking the server', async () => {
    const provisioning = useProvisioning()
    provisioning.database.value = 'Shop'

    await provisioning.load()

    expect(get).not.toHaveBeenCalled()
    expect(provisioning.failure.value?.kind).toBe('error')
  })

  it('asks about every role the migration declares', async () => {
    get.mockResolvedValue(report())
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    // Split on either separator: somebody pasting a list gets whichever their
    // source used, and being told the whole thing is one role name is a worse
    // answer than accepting both.
    provisioning.roles.value = 'role_shop, role_reporting\nrole_etl'

    await provisioning.load()

    expect(get).toHaveBeenCalledWith('/provision/preconditions', {
      params: {
        query: { database: 'shop', role: ['role_shop', 'role_reporting', 'role_etl'] },
      },
    })
  })

  it('reports nothing missing when everything is there', async () => {
    get.mockResolvedValue(report())
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'

    await provisioning.load()

    expect(provisioning.missing.value).toEqual([])
    expect(provisioning.ready.value).toBe(true)
  })

  it('names the database, the owner role and each absent role as missing', async () => {
    get.mockResolvedValue(
      report({
        databaseExists: false,
        ownerRoleExists: false,
        deployerOwnsSchema: false,
        roles: [
          { name: 'role_shop', exists: false },
          { name: 'role_reporting', exists: true },
        ],
      }),
    )
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'

    await provisioning.load()

    expect(provisioning.missing.value).toEqual(['database', 'owner role', 'role_shop'])
    expect(provisioning.ready.value).toBe(false)
  })

  // The membership is only a gap once the database is there: before that it is
  // not a thing that can be missing, and listing it would tell a DBA to fix
  // something that does not exist yet.
  it('does not call the membership missing before the database exists', async () => {
    get.mockResolvedValue(report({ databaseExists: false, deployerOwnsSchema: false }))
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'

    await provisioning.load()

    expect(provisioning.missing.value).not.toContain('deploy membership')
  })

  it('reads the preconditions back after creating, rather than assuming', async () => {
    post.mockResolvedValue({
      data: { objects: [{ kind: 'database', name: 'shop', created: true }] },
      error: undefined,
      response: { status: 200 },
    })
    get.mockResolvedValue(report())

    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    await provisioning.createDatabase()

    expect(post).toHaveBeenCalledWith('/provision/databases', { body: { name: 'shop' } })
    expect(get).toHaveBeenCalledTimes(1)
    expect(provisioning.ready.value).toBe(true)
  })

  it('shows what the server refused and asks nothing further', async () => {
    post.mockResolvedValue({
      data: undefined,
      error: { code: 'provision.not_governed', message: 'That database is Avior own.' },
      response: { status: 403 },
    })

    const provisioning = useProvisioning()
    provisioning.database.value = 'aviordb'
    await provisioning.createDatabase()

    expect(provisioning.failure.value).not.toBeNull()
    expect(get).not.toHaveBeenCalled()
  })

  // A report about the previous database, left on screen beside a new name, is
  // the page saying something untrue.
  it('clears the previous report before asking about another database', async () => {
    get.mockResolvedValueOnce(report())
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    await provisioning.load()
    expect(provisioning.preconditions.value).not.toBeNull()

    get.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    provisioning.database.value = 'other'
    await provisioning.load()

    expect(provisioning.preconditions.value).toBeNull()
    expect(provisioning.failure.value).not.toBeNull()
  })

  // FEAT-076. Both acts name the database the report is about, and read the
  // report back afterwards rather than updating it from their own request.
  it('grants connect on the database the report is about, then reads it back', async () => {
    get.mockResolvedValue(
      report({ roles: [{ name: 'role_shop', exists: true, canConnect: false }] }),
    )
    post.mockResolvedValue({
      data: { objects: [{ kind: 'connect', name: 'role_shop@shop', created: true }] },
      error: undefined,
      response: { status: 200 },
    })
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    await provisioning.load()

    await provisioning.grantConnect('role_shop')

    expect(post).toHaveBeenCalledWith('/provision/databases/{name}/connect', {
      params: { path: { name: 'shop' } },
      body: { role: 'role_shop' },
    })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('prepares the database for writes, then reads it back', async () => {
    get.mockResolvedValue(
      report({
        writes: { writerRole: 'avior_writer', prepared: false, missing: ['capture_table'] },
      }),
    )
    post.mockResolvedValue({
      data: { objects: [{ kind: 'writes', name: 'shop', created: true }] },
      error: undefined,
      response: { status: 200 },
    })
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    await provisioning.load()

    await provisioning.prepareWrites()

    expect(post).toHaveBeenCalledWith('/provision/databases/{name}/writes', {
      params: { path: { name: 'shop' } },
    })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('asks nothing when there is no report to act on', async () => {
    const provisioning = useProvisioning()

    await provisioning.grantConnect('role_shop')
    await provisioning.prepareWrites()

    expect(post).not.toHaveBeenCalled()
  })

  it('shows a refused preparation and does not read back', async () => {
    get.mockResolvedValue(report())
    post.mockResolvedValue({
      data: undefined,
      error: { code: 'provision.writer_missing', message: 'The writer role is not on the server.' },
      response: { status: 409 },
    })
    const provisioning = useProvisioning()
    provisioning.database.value = 'shop'
    await provisioning.load()

    await provisioning.prepareWrites()

    expect(provisioning.failure.value).not.toBeNull()
    expect(get).toHaveBeenCalledTimes(1)
  })
})

describe('describeWriteGaps', () => {
  it('names every gap in the order the server gave', () => {
    expect(
      describeWriteGaps({
        writerRole: 'avior_writer',
        prepared: false,
        missing: ['connect', 'capture_table', 'default_privileges'],
      }),
    ).toBe('CONNECT, the capture table, default privileges for tables a release adds')
  })
})
