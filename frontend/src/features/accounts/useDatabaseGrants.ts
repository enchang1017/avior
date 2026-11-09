import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import { allRoles } from '@/shared/roles'
import type { components } from '@/gen/api'

export type DatabaseGrant = components['schemas']['DatabaseGrant']
export type DatabaseGrantChange = components['schemas']['DatabaseGrantChange']

/**
 * FEAT-058. Which roles reach which governed database.
 *
 * A matrix: every database the server holds down one side, every role across
 * the top, and a tick where a grant exists. Deny by default, so an empty row is
 * a database nobody reaches — which is what a newly added service looks like
 * until somebody grants it.
 */
export const grantableRoles = allRoles

export function useDatabaseGrants() {
  const grants = ref<DatabaseGrant[]>([])
  /**
   * Every governed database on the server, unfiltered.
   *
   * Not the schema browser's list, which answers what the reader's own roles
   * reach — built from that, a database granted to nobody would be missing from
   * the page whose job is to grant it, and that is precisely the state a newly
   * added service database is in.
   */
  const databases = ref<string[]>([])
  const history = ref<DatabaseGrantChange[]>([])
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const changing = ref('')

  const rows = computed(() => {
    const named = new Set<string>(databases.value)
    for (const grant of grants.value) named.add(grant.database)
    return [...named].sort()
  })

  function granted(database: string, role: string): boolean {
    return grants.value.some((grant) => grant.database === database && grant.role === role)
  }

  async function load(): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const [matrix, governed, changes] = await Promise.all([
        api.GET('/databases/grants'),
        api.GET('/databases/governed'),
        api.GET('/databases/grants/history', { params: { query: { limit: 50 } } }),
      ])
      if (matrix.data === undefined) {
        failure.value = classify(matrix.response.status, matrix.error)
        return
      }
      grants.value = matrix.data
      // Both need permission.manage and the matrix does not. Somebody reading
      // the page to find out why they were refused gets the matrix and neither
      // of these, which is the whole of what they need.
      databases.value = governed.data ?? []
      history.value = changes.data ?? []
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function set(database: string, role: string, grant: boolean): Promise<void> {
    changing.value = database + '/' + role
    failure.value = null
    try {
      const { data, error, response } = await api.PUT('/databases/{database}/roles/{role}', {
        params: { path: { database, role } },
        body: { granted: grant },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      // The server answers with the whole matrix, so the page shows what is
      // stored rather than what was asked for.
      grants.value = data
      void refreshHistory()
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      changing.value = ''
    }
  }

  // Read again after a change, so the record on screen includes what was just
  // done rather than needing a reload to show it.
  async function refreshHistory(): Promise<void> {
    try {
      const { data } = await api.GET('/databases/grants/history', {
        params: { query: { limit: 50 } },
      })
      history.value = data ?? []
    } catch {
      // The grant itself succeeded. A stale record on screen is not worth
      // reporting as a failure of the thing that worked.
    }
  }

  return { grants, rows, granted, history, failure, busy, changing, load, set }
}
