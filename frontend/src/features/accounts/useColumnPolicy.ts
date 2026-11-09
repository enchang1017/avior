import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import { allRoles } from '@/shared/roles'
import type { components } from '@/gen/api'

export type ColumnRestriction = components['schemas']['ColumnRestriction']
export type ColumnUnlock = components['schemas']['ColumnUnlock']
export type ColumnChange = components['schemas']['ColumnChange']

/**
 * FEAT-055. Which columns are restricted, and who has been unlocked for them.
 *
 * A restricted column is refused to every role — including DBA and
 * administrator — until an unlock names one. The page is therefore two lists
 * rather than a matrix: the restriction is a fact about the column, and each
 * unlock is a decision somebody made about one role, with a reason and possibly
 * an end.
 */
export const unlockableRoles = allRoles

export function useColumnPolicy() {
  const restrictions = ref<ColumnRestriction[]>([])
  /**
   * What the picker offers, read through the administration's own endpoints.
   *
   * Not the schema browser's, which answers what the reader's roles reach: an
   * administrator holding no database grant could not pick the column they were
   * asked to restrict.
   */
  const pickDatabases = ref<string[]>([])
  const pickTables = ref<{ schema: string; name: string }[]>([])
  const pickColumns = ref<string[]>([])
  const history = ref<ColumnChange[]>([])
  const unlocks = ref<ColumnUnlock[]>([])
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const changing = ref('')

  /** The unlocks that apply to one column, expired ones included: an unlock
   * that has ended is shown as ended rather than hidden, because somebody
   * having had it is the thing worth seeing. */
  function unlocksFor(restriction: ColumnRestriction): ColumnUnlock[] {
    return unlocks.value.filter(
      (unlock) =>
        unlock.database === restriction.database &&
        unlock.object === restriction.object &&
        unlock.column === restriction.column,
    )
  }

  function unlockFor(restriction: ColumnRestriction, role: string): ColumnUnlock | undefined {
    return unlocksFor(restriction).find((unlock) => unlock.role === role)
  }

  /** Whether the role reads it today: an unlock that has expired does not. */
  function reads(restriction: ColumnRestriction, role: string): boolean {
    const unlock = unlockFor(restriction, role)
    return unlock !== undefined && !unlock.expired
  }

  const databases = computed(() => [...new Set(restrictions.value.map((r) => r.database))].sort())

  function apply(policy: components['schemas']['ColumnPolicy']): void {
    restrictions.value = policy.restrictions
    unlocks.value = policy.unlocks
    // Read again after a change, so the record on screen includes what was just
    // done rather than needing a reload to show it.
    void loadHistory()
  }

  /** Needs permission.manage, unlike the policy itself. Silent on failure: a
   * reader who may not see the record still gets the page. */
  async function loadHistory(): Promise<void> {
    try {
      const { data } = await api.GET('/columns/history', { params: { query: { limit: 50 } } })
      history.value = data ?? []
    } catch {
      history.value = []
    }
  }

  /** The governed databases, for the picker. Silent on failure: the page's own
   * read reports what went wrong, and a picker that cannot be filled is not a
   * second banner. */
  async function loadDatabases(): Promise<void> {
    try {
      const { data } = await api.GET('/databases/governed')
      pickDatabases.value = data ?? []
    } catch {
      pickDatabases.value = []
    }
  }

  async function loadTables(database: string): Promise<void> {
    pickTables.value = []
    pickColumns.value = []
    if (database === '') return
    try {
      const { data } = await api.GET('/admin/schema/tables', {
        params: { query: { instanceId, database } },
      })
      pickTables.value = (data ?? []).map((table) => ({
        schema: table.schema,
        name: table.name,
      }))
    } catch {
      pickTables.value = []
    }
  }

  async function loadColumns(database: string, schema: string, table: string): Promise<void> {
    pickColumns.value = []
    if (database === '' || table === '') return
    try {
      const { data } = await api.GET('/admin/schema/columns', {
        params: { query: { instanceId, database, schema, table } },
      })
      pickColumns.value = data ?? []
    } catch {
      pickColumns.value = []
    }
  }

  async function load(): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.GET('/columns/restrictions')
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      apply(data)
      void loadDatabases()
      void loadHistory()
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function setUnlocked(
    restriction: ColumnRestriction,
    role: string,
    unlocked: boolean,
    reason: string,
    expiresAt: string,
  ): Promise<boolean> {
    changing.value = key(restriction) + '/' + role
    failure.value = null
    try {
      const { data, error, response } = await api.PUT('/columns/unlock', {
        body: {
          database: restriction.database,
          object: restriction.object,
          column: restriction.column,
          role,
          unlocked,
          ...(reason === '' ? {} : { reason }),
          ...(expiresAt === '' ? {} : { expiresAt: new Date(expiresAt).toISOString() }),
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      apply(data)
      return true
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      changing.value = ''
    }
  }

  async function setRestricted(
    database: string,
    object: string,
    column: string,
    restricted: boolean,
    reason: string,
  ): Promise<boolean> {
    changing.value = database + '/' + object + '.' + column
    failure.value = null
    try {
      const { data, error, response } = await api.PUT('/columns/restriction', {
        body: {
          database,
          object,
          column,
          restricted,
          ...(reason === '' ? {} : { reason }),
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      apply(data)
      return true
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      changing.value = ''
    }
  }

  return {
    restrictions,
    unlocks,
    databases,
    history,
    pickDatabases,
    pickTables,
    pickColumns,
    loadTables,
    loadColumns,
    unlocksFor,
    unlockFor,
    reads,
    failure,
    busy,
    changing,
    load,
    setUnlocked,
    setRestricted,
  }
}

/**
 * The instance the picker reads through.
 *
 * One instance for now, the same constant the query console carries. FEAT-012
 * turns it into a picker fed by `db_instance`.
 */
const instanceId = '00000000-0000-0000-0000-0000000000a1'

export function key(restriction: ColumnRestriction): string {
  return `${restriction.database}/${restriction.object}.${restriction.column}`
}
