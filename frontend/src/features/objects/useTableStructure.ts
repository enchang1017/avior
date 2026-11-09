import { computed, ref, watch } from 'vue'

import { api, classify, unreachable, type Failure, type TableRef } from '@/shared/api'
import type { components } from '@/gen/api'

export type TableStructure = components['schemas']['TableStructure']
export type TableColumn = components['schemas']['TableColumn']

/**
 * useTableStructure browses to one relation and reads it as its four sections.
 *
 * The list is every table and view, from the same endpoint the query console
 * uses. A view has columns, grants and a comment, and an operator asking about
 * one is asking the same question — offering only tables would refuse the
 * answer over a distinction they did not make.
 */
export function useTableStructure(instanceId: string) {
  const databases = ref<string[]>([])
  const tables = ref<TableRef[]>([])
  const database = ref<string | null>(null)
  const selected = ref<TableRef | null>(null)
  const structure = ref<TableStructure | null>(null)

  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  /** Whether the relation has anything to say in each section. */
  const sections = computed(() => ({
    columns: structure.value?.columns.length ?? 0,
    indexes: structure.value?.indexes.length ?? 0,
    grants: structure.value?.grants.length ?? 0,
  }))

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

  async function chooseDatabase(next: string | null): Promise<void> {
    database.value = next
    // Everything downstream belonged to the last database. A stale list is how
    // somebody reads the shape of the wrong object, and the names look
    // plausible enough not to notice.
    tables.value = []
    selected.value = null
    structure.value = null
    if (next === null) return

    await guard(async () => {
      const { data, error, response } = await api.GET('/schema/tables', {
        params: { query: { instanceId, database: next } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      tables.value = data
    })
  }

  async function choose(table: TableRef | null): Promise<void> {
    selected.value = table
    structure.value = null
    if (table === null || database.value === null) return

    await guard(async () => {
      const { data, error, response } = await api.GET('/objects/table', {
        params: {
          query: {
            instanceId,
            database: database.value ?? '',
            schema: table.schema,
            name: table.name,
          },
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      structure.value = data
    })
  }

  // One database is no choice at all, so it is made rather than asked for.
  watch(databases, (available) => {
    if (database.value === null && available.length === 1) {
      void chooseDatabase(available[0] ?? null)
    }
  })

  return {
    databases,
    tables,
    database,
    selected,
    structure,
    sections,
    failure,
    busy,
    loadDatabases,
    chooseDatabase,
    choose,
  }
}
