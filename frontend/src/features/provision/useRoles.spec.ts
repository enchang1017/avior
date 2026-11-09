import { describe, expect, it, vi, beforeEach } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
const del = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post, DELETE: del } }
})

const { useRoles } = await import('./useRoles')

const listed = [
  {
    name: 'role_shop',
    kind: 'group',
    canLogin: false,
    memberOf: [],
    members: ['shop_app'],
    grants: [],
  },
  {
    name: 'shop_app',
    kind: 'login',
    canLogin: true,
    memberOf: ['role_shop'],
    members: [],
    grants: [{ database: 'shop', tables: 12, select: 12, insert: 0, update: 0, delete: 0 }],
  },
  {
    name: 'shop_schema_owner',
    kind: 'owner',
    canLogin: false,
    memberOf: [],
    members: [],
    grants: [],
  },
]

function ok<T>(data: T, status = 200) {
  return { data, error: undefined, response: { status, ok: true } }
}

function refused(status: number, code: string) {
  return { data: undefined, error: { code, message: code }, response: { status, ok: false } }
}

beforeEach(() => {
  get.mockReset()
  post.mockReset()
  del.mockReset()
})

describe('useRoles', () => {
  it('lists the roles about one database and offers only the groups as something a login can join', async () => {
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    await store.load()
    expect(get).not.toHaveBeenCalled()
    store.database.value = 'shop'
    await store.load()
    expect(get).toHaveBeenCalledWith('/provision/roles', {
      params: { query: { database: 'shop' } },
    })
    expect(store.roles.value).toEqual(listed)
    expect(store.groups.value.map((role) => role.name)).toEqual(['role_shop'])
    expect(store.loaded.value).toBe(true)
  })

  it('keeps the password from creating a login until dismissed, then reloads', async () => {
    post.mockResolvedValue(ok({ name: 'svc', password: 'avior-pw', memberOf: ['role_shop'] }, 201))
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'

    expect(await store.createLogin(' svc ', ['role_shop'])).toBe(true)
    expect(post).toHaveBeenCalledWith('/provision/logins', {
      body: { name: 'svc', memberOf: ['role_shop'] },
    })
    expect(store.minted.value?.password).toBe('avior-pw')
    expect(get).toHaveBeenCalledTimes(1)

    store.dismissMinted()
    expect(store.minted.value).toBeNull()
  })

  it('reports a conflict as the failure rather than a password', async () => {
    post.mockResolvedValue(refused(409, 'provision.role_exists'))
    const store = useRoles()
    expect(await store.createLogin('svc', [])).toBe(false)
    expect(store.minted.value).toBeNull()
    expect(store.failure.value).toMatchObject({ code: 'provision.role_exists' })
  })

  it('rotating hands back a new password for the named login', async () => {
    post.mockResolvedValue(ok({ name: 'svc', password: 'new-pw', memberOf: [] }))
    const store = useRoles()
    await store.rotate('svc')
    expect(post).toHaveBeenCalledWith('/provision/logins/{name}/password', {
      params: { path: { name: 'svc' } },
    })
    expect(store.minted.value?.password).toBe('new-pw')
  })

  it('dropping reloads the list, and a refusal is kept with its code', async () => {
    del.mockResolvedValueOnce({ error: undefined, response: { status: 204, ok: true } })
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.drop('svc')
    expect(del).toHaveBeenCalledWith('/provision/roles/{name}', {
      params: { path: { name: 'svc' } },
    })
    expect(get).toHaveBeenCalledTimes(1)

    del.mockResolvedValueOnce(refused(403, 'provision.protected_role'))
    await store.drop('avior_governed')
    expect(store.failure.value).toMatchObject({ code: 'provision.protected_role' })
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('a server that cannot be reached is a failure, not an exception', async () => {
    get.mockRejectedValue(new TypeError('Failed to fetch'))
    post.mockRejectedValue(new TypeError('Failed to fetch'))
    del.mockRejectedValue(new TypeError('Failed to fetch'))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    expect(store.failure.value).not.toBeNull()
    expect(await store.createLogin('svc', [])).toBe(false)
    await store.rotate('svc')
    await store.drop('svc')
    expect(store.busy.value).toBe(false)
  })

  // FEAT-074 AC-6: the first search is a question in flight, not "name a database".
  it('says it is searching only while a search is in flight', async () => {
    let answer: (value: unknown) => void = () => undefined
    get.mockReturnValue(new Promise((resolve) => (answer = resolve)))
    const store = useRoles()
    store.database.value = 'shop'

    const loading = store.load()
    expect(store.searching.value).toBe(true)
    expect(store.loaded.value).toBe(false)
    answer({ data: listed, response: { status: 200 } })
    await loading

    expect(store.searching.value).toBe(false)
    expect(store.loaded.value).toBe(true)
  })

  // FEAT-076 AC-14. A row says what a role may do in the database the list was
  // read for, which stops being the field the moment somebody types in it.
  it('remembers the database the list was read for, not what the field says now', async () => {
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()

    store.database.value = 'other'

    expect(store.loadedDatabase.value).toBe('shop')
  })

  // FEAT-076 AC-18. The dialog reads the database the list was read for, and a
  // late answer about one role never lands under another's name.
  it('opens a role on the database the list was read for', async () => {
    get.mockResolvedValueOnce(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    const access = { role: 'role_shop', database: 'shop', canConnect: false }
    get.mockResolvedValueOnce(ok(access))

    await store.open('role_shop')

    expect(get).toHaveBeenLastCalledWith('/provision/roles/{name}/access', {
      params: { path: { name: 'role_shop' }, query: { database: 'shop' } },
    })
    expect(store.access.value).toEqual(access)
    store.close()
    expect(store.chosen.value).toBeNull()
    expect(store.access.value).toBeNull()
  })

  it('drops an answer that arrives after another role was chosen', async () => {
    get.mockResolvedValueOnce(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    let answer: (value: unknown) => void = () => {}
    get.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)))
    const first = store.open('role_shop')
    get.mockResolvedValueOnce(ok({ role: 'shop_app' }))
    await store.open('shop_app')
    answer(ok({ role: 'role_shop' }))
    await first

    expect(store.access.value).toEqual({ role: 'shop_app' })
  })

  it('shows a refused read in the dialog', async () => {
    get.mockResolvedValueOnce(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    get.mockResolvedValueOnce(refused(404, 'provision.role_not_found'))

    await store.open('gone')

    expect(store.accessFailure.value).not.toBeNull()
  })

  it('grants connect from the dialog, then reads the list and the role back', async () => {
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    await store.open('role_shop')
    post.mockResolvedValueOnce(ok({ objects: [] }))

    await store.grantConnect('role_shop')

    expect(post).toHaveBeenCalledWith('/provision/databases/{name}/connect', {
      params: { path: { name: 'shop' } },
      body: { role: 'role_shop' },
    })
    expect(get).toHaveBeenCalledTimes(4)
  })

  it('reports a refused grant and reads nothing back', async () => {
    get.mockResolvedValue(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    post.mockResolvedValueOnce(refused(403, 'provision.protected_role'))

    await store.grantConnect('avior_writer')

    expect(store.failure.value).not.toBeNull()
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('opens a role it already read from memory, and forgets on the next search', async () => {
    get.mockResolvedValueOnce(ok(listed))
    const store = useRoles()
    store.database.value = 'shop'
    await store.load()
    get.mockResolvedValueOnce(ok({ role: 'role_shop' }))
    await store.open('role_shop')
    store.close()

    await store.open('role_shop')
    expect(get).toHaveBeenCalledTimes(2)
    expect(store.access.value).toEqual({ role: 'role_shop' })
    expect(store.reading.value).toBe(false)

    get.mockResolvedValueOnce(ok(listed))
    await store.load()
    get.mockResolvedValueOnce(ok({ role: 'role_shop', fresh: true }))
    await store.open('role_shop')
    expect(get).toHaveBeenCalledTimes(4)
  })
})
