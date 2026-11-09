import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import { assignableRoles } from '@/shared/roles'
import type { components } from '@/gen/api'

export type Operator = components['schemas']['Operator']
export type RoleDetail = components['schemas']['RoleDetail']

/**
 * The order the roles are offered in, when the server has not answered yet.
 *
 * The columns have to be drawn before `/roles` returns or the table reflows
 * under the reader. What each one opens is never guessed: that comes from the
 * server, which builds it from the same table the guard reads.
 */
export const fallbackRoles = assignableRoles

export function useOperators() {
  const operators = ref<Operator[]>([])
  const roles = ref<RoleDetail[]>([])
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const loaded = ref(false)
  /**
   * Which operator is mid-change, so one row's switch can be disabled without
   * freezing the page. Empty means none.
   */
  const changing = ref('')
  const search = ref('')

  const shown = computed(() => {
    const term = search.value.trim().toLowerCase()
    if (term === '') return operators.value
    return operators.value.filter((operator) => operator.username.toLowerCase().includes(term))
  })

  /**
   * The roles the page may hand out, in the server's order.
   *
   * Empty until `/roles` answers, and the caller falls back to the names it
   * knows so the table has columns to draw.
   */
  const administrable = computed(() => roles.value.filter((role) => role.administrable))

  /**
   * loadRoles reads the role model, for the columns and for the guide.
   *
   * A failure is not a banner: the page's own read reports what went wrong, and
   * the columns fall back to the names this build knows. Being unable to
   * explain a role is not a reason to stop somebody granting one.
   */
  async function loadRoles(): Promise<void> {
    try {
      const { data } = await api.GET('/roles')
      roles.value = data ?? []
    } catch {
      roles.value = []
    }
  }

  async function load(): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.GET('/operators')
      if (data === undefined) {
        operators.value = []
        failure.value = classify(response.status, error)
        return
      }
      operators.value = data
      loaded.value = true
    } catch (thrown) {
      operators.value = []
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * replace swaps one operator for the version the server sent back.
   *
   * The response is the authority on what the operator now holds rather than
   * the request being replayed onto the row: a grant the server declined to
   * change — one already held — comes back unchanged, and a row edited
   * optimistically would claim a change that did not happen.
   */
  function replace(updated: Operator): void {
    operators.value = operators.value.map((operator) =>
      operator.id === updated.id ? updated : operator,
    )
  }

  async function setRole(operator: Operator, role: string, granted: boolean): Promise<void> {
    changing.value = operator.id
    failure.value = null
    try {
      const { data, error, response } = await api.PUT('/operators/{operatorId}/roles/{role}', {
        params: { path: { operatorId: operator.id, role } },
        body: { granted },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      replace(data)
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      changing.value = ''
    }
  }

  async function setDisabled(operator: Operator, disabled: boolean): Promise<void> {
    changing.value = operator.id
    failure.value = null
    try {
      const { data, error, response } = await api.PUT('/operators/{operatorId}/disabled', {
        params: { path: { operatorId: operator.id } },
        body: { disabled },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      replace(data)
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      changing.value = ''
    }
  }

  return {
    operators,
    roles,
    administrable,
    loadRoles,
    shown,
    search,
    failure,
    busy,
    loaded,
    changing,
    load,
    setRole,
    setDisabled,
  }
}

export function holdsRole(operator: Operator, role: string): boolean {
  return operator.roles.includes(role)
}
