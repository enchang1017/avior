import { watch } from 'vue'
import { useRoute } from 'vue-router'

import type { TableRef } from '@/shared/api'

type Browser = {
  database: { value: string | null }
  chooseDatabase: (name: string | null) => Promise<void>
}

export type RouteTarget = { database: string; table: TableRef | null }

const text = (value: unknown) => (typeof value === 'string' ? value.trim() : '')

/** What a link from the palette asks the console to point at, when it asks for anything. */
export function targetFromQuery(query: Record<string, unknown>): RouteTarget | null {
  const database = text(query['database'])
  if (database === '') return null
  const schema = text(query['schema'])
  const name = text(query['table'])
  const kind = text(query['kind']) === 'view' ? 'view' : 'table'
  return { database, table: schema !== '' && name !== '' ? { schema, name, kind } : null }
}

/**
 * Points the console at the database and table a link names, exactly as picking
 * them in the tree would: the tree selects them, the TARGET readout follows, and
 * choosing the table writes its statement.
 */
export function useRouteTarget(
  browser: Browser,
  chooseTable: (table: TableRef | null) => Promise<void>,
): void {
  const route = useRoute()
  watch(
    () => targetFromQuery(route.query),
    async (target) => {
      if (target === null) return
      if (browser.database.value !== target.database) await browser.chooseDatabase(target.database)
      if (target.table !== null) await chooseTable(target.table)
    },
    { immediate: true },
  )
}
