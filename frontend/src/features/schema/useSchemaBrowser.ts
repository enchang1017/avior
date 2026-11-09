import { computed, ref } from 'vue'

import {
  api,
  classify,
  unreachable,
  type ColumnRef,
  type Failure,
  type TableRef,
} from '@/shared/api'

/**
 * Whether a rendered type is a date or a time.
 *
 * Matched on the rendered type rather than on an OID, because that is what the
 * columns endpoint returns — and it is what the operator is reading beside the
 * name, so the two agree about which columns are dates.
 */
function isTemporal(type: string): boolean {
  const rendered = type.toLowerCase()
  return (
    rendered.startsWith('timestamp') ||
    rendered.startsWith('date') ||
    rendered.startsWith('time') ||
    rendered.startsWith('interval')
  )
}

/**
 * useSchemaBrowser walks instance to database to table to columns.
 *
 * Each step clears everything downstream. Leaving a stale column list on screen
 * after the table changed is how an operator builds a statement over the wrong
 * object, and the names would look plausible enough not to notice.
 */
export function useSchemaBrowser(instanceId: string) {
  const databases = ref<string[]>([])
  const tables = ref<TableRef[]>([])
  const columns = ref<ColumnRef[]>([])

  const database = ref<string | null>(null)
  const table = ref<TableRef | null>(null)
  const selected = ref<Set<string>>(new Set())

  /** The column to sort by, empty for none, and which way. */
  const orderBy = ref('')
  const descending = ref(true)

  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  const selectableColumns = computed(() => columns.value.filter((column) => !column.denied))
  const selectedColumns = computed(() =>
    columns.value.filter((column) => selected.value.has(column.name)).map((column) => column.name),
  )
  const allSelected = computed(
    () =>
      selectableColumns.value.length > 0 &&
      selectableColumns.value.every((column) => selected.value.has(column.name)),
  )

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
    tables.value = []
    await chooseTable(null)
    if (name === null) return

    await guard(async () => {
      const { data, error, response } = await api.GET('/schema/tables', {
        params: { query: { instanceId, database: name } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      tables.value = data
    })
  }

  async function chooseTable(next: TableRef | null): Promise<void> {
    table.value = next
    columns.value = []
    selected.value = new Set()
    if (next === null || database.value === null) return

    const currentDatabase = database.value
    await guard(async () => {
      const { data, error, response } = await api.GET('/schema/columns', {
        params: {
          query: {
            instanceId,
            database: currentDatabase,
            schema: next.schema,
            table: next.name,
          },
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      columns.value = data
      // Every readable column starts selected, which is what an operator wants
      // nine times in ten and matches the predecessor's Insert button.
      selected.value = new Set(data.filter((column) => !column.denied).map((c) => c.name))
      // A new table has different columns, so an ordering carried over from the
      // last one would name a column this table does not have.
      orderBy.value = ''
      descending.value = true
    })
  }

  function toggle(name: string): void {
    const column = columns.value.find((c) => c.name === name)
    if (column === undefined || column.denied) return

    const next = new Set(selected.value)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    selected.value = next
  }

  function toggleAll(): void {
    selected.value = allSelected.value
      ? new Set()
      : new Set(selectableColumns.value.map((column) => column.name))
  }

  /**
   * buildStatement asks the server to render the SELECT. The quoting rules, the
   * explicit column list and the row bound live with the guard that enforces
   * them, so what comes back is guaranteed to be accepted; building the string
   * here would mean maintaining the same rules twice and being refused when they
   * drift.
   */
  /**
   * The columns worth offering as an ordering, most likely first.
   *
   * A timestamp is what an operator almost always means by "order by": they
   * want the newest rows. Everything else stays on the list — sorting by an
   * amount or a name is ordinary — but the dates go to the top rather than
   * being hunted for among forty columns.
   *
   * A denied column is not offered at all. ORDER BY reads the column, so the
   * server refuses it, and suggesting one would be suggesting a statement that
   * cannot run.
   */
  const orderableColumns = computed(() => {
    const readable = columns.value.filter((column) => !column.denied)
    const byTime = readable.filter((column) => isTemporal(column.type))
    const rest = readable.filter((column) => !isTemporal(column.type))
    return [...byTime, ...rest].map((column) => column.name)
  })

  const temporalColumns = computed(() =>
    columns.value.filter((column) => !column.denied && isTemporal(column.type)).map((c) => c.name),
  )

  async function buildStatement(top: number): Promise<string | null> {
    if (database.value === null || table.value === null || selectedColumns.value.length === 0) {
      return null
    }
    const body = {
      instanceId,
      database: database.value,
      schema: table.value.schema,
      table: table.value.name,
      columns: selectedColumns.value,
      top,
      ...(orderBy.value === '' ? {} : { orderBy: orderBy.value, descending: descending.value }),
    }
    return await guard(async () => {
      const { data, error, response } = await api.POST('/query/template', { body })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return null
      }
      return data.statement
    })
  }

  return {
    orderBy,
    descending,
    orderableColumns,
    temporalColumns,
    databases,
    tables,
    columns,
    database,
    table,
    selected,
    selectableColumns,
    selectedColumns,
    allSelected,
    failure,
    busy,
    loadDatabases,
    chooseDatabase,
    chooseTable,
    toggle,
    toggleAll,
    buildStatement,
  }
}
