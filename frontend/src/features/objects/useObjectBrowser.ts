import { computed, ref, watch } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type ObjectType = components['schemas']['ObjectType']
export type ObjectRef = components['schemas']['ObjectRef']
export type ObjectDefinition = components['schemas']['ObjectDefinition']

/**
 * The label each type is picked under. An index is read through the table that
 * carries it, so the operator is asked for a table; the predecessor's menu says the
 * same and the operators know it.
 */
export const objectTypeLabels: Record<ObjectType, string> = {
  view: 'View',
  stored_procedure: 'Stored Procedure',
  user_defined_type: 'User Defined Type',
  function: 'Function',
  index: 'Table',
}

/**
 * useObjectBrowser walks database to object to definition for one type.
 *
 * Each step clears everything downstream, for the reason the schema browser
 * does: a definition left on screen after the database changed is read as
 * belonging to the new one.
 *
 * The type arrives as a getter, not a value. The predecessor gives each of the five its
 * own menu entry and its own URL, and Vue Router reuses one component instance
 * across them rather than remounting, so a type captured at setup would stay at
 * whichever entry the operator opened first: the heading would follow the route
 * and the list would not.
 */
export function useObjectBrowser(instanceId: string, objectType: () => ObjectType) {
  const databases = ref<string[]>([])
  const objects = ref<ObjectRef[]>([])
  const definition = ref<ObjectDefinition | null>(null)

  const database = ref<string | null>(null)
  const selected = ref<ObjectRef | null>(null)
  const filter = ref('')

  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  const label = computed(() => objectTypeLabels[objectType()])

  const visible = computed(() => {
    const needle = filter.value.trim().toLowerCase()
    if (needle === '') return objects.value
    return objects.value.filter((object) => object.name.toLowerCase().includes(needle))
  })

  async function guard<T>(work: () => Promise<T>): Promise<T | null> {
    busy.value = true
    failure.value = null
    try {
      return await work()
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return null
    } finally {
      busy.value = false
    }
  }

  async function loadDatabases(): Promise<void> {
    await guard(async () => {
      const { data, error, response } = await api.GET('/schema/databases', {
        params: { query: { instanceId } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      databases.value = data
    })
  }

  async function chooseDatabase(name: string | null): Promise<void> {
    database.value = name
    objects.value = []
    filter.value = ''
    await choose(null)
    if (name === null) return

    await guard(async () => {
      const { data, error, response } = await api.GET('/objects', {
        params: { query: { instanceId, database: name, type: objectType() } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      objects.value = data
    })
  }

  async function choose(next: ObjectRef | null): Promise<void> {
    selected.value = next
    definition.value = null
    if (next === null || database.value === null) return

    const currentDatabase = database.value
    await guard(async () => {
      const { data, error, response } = await api.GET('/objects/definition', {
        params: {
          query: {
            instanceId,
            database: currentDatabase,
            type: objectType(),
            schema: next.schema,
            name: next.name,
          },
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      definition.value = data
    })
  }

  /**
   * Opens the browser where a link points: a database, and optionally one
   * object in it. The definition is read through the same request as a click
   * on the list, so it is audited exactly as that read is.
   */
  async function openAt(name: string, object: ObjectRef | null): Promise<void> {
    await chooseDatabase(name)
    if (object !== null && database.value === name) await choose(object)
  }

  // Switching type keeps the database and replaces everything under it, which
  // is what the menu implies: the operator asked for a different kind of thing
  // in the same place.
  watch(objectType, () => {
    void chooseDatabase(database.value)
  })

  return {
    databases,
    objects,
    visible,
    definition,
    database,
    selected,
    filter,
    failure,
    busy,
    label,
    loadDatabases,
    chooseDatabase,
    choose,
    openAt,
  }
}
