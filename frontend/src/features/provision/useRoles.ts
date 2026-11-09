import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type ProvisionedRole = components['schemas']['ProvisionedRole']
export type ProvisionedLogin = components['schemas']['ProvisionedLogin']
export type DatabaseGrantSummary = components['schemas']['DatabaseGrantSummary']
export type RoleAccess = components['schemas']['RoleAccess']

/**
 * useRoles is the roles Avior administers, and the three things done to them.
 * FEAT-061.
 *
 * The password follows the token page's one rule: it is held here from the
 * response until dismissed, shown in one place, and never asked for again
 * because the server cannot answer.
 */
export function useRoles() {
  const roles = ref<ProvisionedRole[]>([])
  const minted = ref<ProvisionedLogin | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const loaded = ref(false)
  /** A search in flight, apart from the other work that sets busy. */
  const searching = ref(false)
  /**
   * What each role opened in this search may do, so opening it again is
   * instant. Forgotten on the next search and after any act, either of which
   * can change the answer.
   */
  const opened = new Map<string, RoleAccess>()

  /**
   * The roles a new login can be made a member of: the groups a project's
   * migrations grant to. Never an owner role — a member of one could ALTER
   * and DROP every table, and that membership is the deploy credential's alone.
   */
  const groups = computed(() => roles.value.filter((role) => role.kind === 'group'))

  /** The database the list is about. Empty until somebody asks. */
  const database = ref('')
  /**
   * The database the list on screen was read for, which the field stops being
   * the moment somebody types. A role's row says what it may do there.
   */
  const loadedDatabase = ref('')

  async function load(): Promise<void> {
    if (database.value.trim() === '') return
    opened.clear()
    busy.value = true
    searching.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.GET('/provision/roles', {
        params: { query: { database: database.value.trim() } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      roles.value = data
      loadedDatabase.value = database.value.trim()
      loaded.value = true
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
      searching.value = false
    }
  }

  async function createLogin(name: string, memberOf: string[]): Promise<boolean> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.POST('/provision/logins', {
        body: { name: name.trim(), memberOf },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      minted.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      busy.value = false
    }
    await load()
    return true
  }

  async function rotate(name: string): Promise<void> {
    opened.clear()
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.POST('/provision/logins/{name}/password', {
        params: { path: { name } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      minted.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function drop(name: string): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { error, response } = await api.DELETE('/provision/roles/{name}', {
        params: { path: { name } },
      })
      if (!response.ok) {
        failure.value = classify(response.status, error)
        return
      }
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return
    } finally {
      busy.value = false
    }
    await load()
  }

  function dismissMinted(): void {
    minted.value = null
  }

  /**
   * The role the dialog is open on, and what it may do in the database the
   * list was read for. FEAT-076 AC-18. Cleared rather than kept while the next
   * one loads, so the dialog never shows one role's grants under another's
   * name.
   */
  const chosen = ref<string | null>(null)
  const access = ref<RoleAccess | null>(null)
  const accessFailure = ref<Failure | null>(null)
  const reading = ref(false)

  async function open(name: string): Promise<void> {
    chosen.value = name
    accessFailure.value = null
    const cached = opened.get(name)
    if (cached !== undefined) {
      access.value = cached
      reading.value = false
      return
    }
    access.value = null
    const db = loadedDatabase.value
    if (db === '') return
    reading.value = true
    try {
      const { data, error, response } = await api.GET('/provision/roles/{name}/access', {
        params: { path: { name }, query: { database: db } },
      })
      if (chosen.value !== name) return
      if (data === undefined) {
        accessFailure.value = classify(response.status, error)
        return
      }
      opened.set(name, data)
      access.value = data
    } catch (thrown) {
      if (chosen.value === name) accessFailure.value = unreachable(thrown)
    } finally {
      if (chosen.value === name) reading.value = false
    }
  }

  /** FEAT-076 AC-2, from the dialog: CONNECT on the database the list is about. */
  async function grantConnect(name: string): Promise<void> {
    const db = loadedDatabase.value
    if (db === '') return
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.POST('/provision/databases/{name}/connect', {
        params: { path: { name: db } },
        body: { role: name },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return
    } finally {
      busy.value = false
    }
    await load()
    if (chosen.value === name) await open(name)
  }

  function close(): void {
    chosen.value = null
    access.value = null
    accessFailure.value = null
  }

  return {
    database,
    loadedDatabase,
    roles,
    groups,
    minted,
    failure,
    busy,
    loaded,
    searching,
    load,
    createLogin,
    rotate,
    drop,
    dismissMinted,
    chosen,
    access,
    accessFailure,
    reading,
    open,
    close,
    grantConnect,
  }
}
