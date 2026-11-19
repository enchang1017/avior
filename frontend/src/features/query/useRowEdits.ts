import { computed, ref } from 'vue'
import {
  api,
  classify,
  describeFailure,
  unreachable,
  type ColumnRef,
  type QueryResult,
  type TableRef,
} from '@/shared/api'
import { cellValue } from './cellValue'

export type RowDraft = {
  id: string
  keyLabel: string
  database: string
  schema: string
  table: string
  original: Record<string, string | null>
  changes: Record<string, string | null>
  statement: string
  error: string
}

export function supportedCellType(type: string): boolean {
  let value = type.toLowerCase()
  if (value.endsWith('[]')) {
    value = value.slice(0, -2)
    if (value === 'json') return false
  }
  value = value.split('(')[0]?.trim() ?? ''
  return [
    'smallint',
    'integer',
    'bigint',
    'numeric',
    'decimal',
    'real',
    'double precision',
    'boolean',
    'text',
    'character varying',
    'character',
    'uuid',
    'date',
    'time',
    'time without time zone',
    'time with time zone',
    'timestamp',
    'timestamp without time zone',
    'timestamp with time zone',
    'interval',
    'json',
    'jsonb',
  ].includes(value)
}

export function editUnavailable(
  table: TableRef | null,
  columns: ColumnRef[],
  result: QueryResult | null,
): string {
  if (!table || table.kind !== 'table') return 'Only ordinary tables can be edited.'
  if (columns.length === 0 || !columns.some((column) => (column.primaryKeyPosition ?? 0) > 0))
    return 'This table has no readable primary key.'
  if (
    columns.some(
      (column) => (column.primaryKeyPosition ?? 0) > 0 && column.generated && !column.identity,
    )
  )
    return 'Tables with expression-generated primary keys are read-only.'
  if (columns.some((column) => column.denied) || result?.columns.some((column) => column.masked))
    return 'Restricted columns make this table read-only.'
  if (columns.some((column) => !supportedCellType(column.type)))
    return 'This table contains a type that cannot be edited in the grid.'
  if (
    !result ||
    result.columns.length !== columns.length ||
    columns.some((column) => !result.columns.some((field) => field.name === column.name))
  )
    return 'Select all columns to edit a complete row.'
  return ''
}

function snapshot(result: QueryResult, index: number): Record<string, string | null> {
  const row = result.rows[index]
  return Object.fromEntries(
    result.columns.map((column, i) => [
      column.name,
      row?.[i] === null || row?.[i] === undefined ? null : cellValue(row[i], column).raw,
    ]),
  )
}
function identity(
  database: string,
  table: TableRef,
  columns: ColumnRef[],
  original: Record<string, string | null>,
): string {
  const key = columns
    .filter((column) => (column.primaryKeyPosition ?? 0) > 0)
    .sort((a, b) => (a.primaryKeyPosition ?? 0) - (b.primaryKeyPosition ?? 0))
    .map((column) => original[column.name])
  return JSON.stringify([database, table.schema, table.name, key])
}

/** Row drafts retain their original snapshot across pages and tables. */
export function useRowEdits() {
  const drafts = ref<RowDraft[]>([])
  const busy = ref(false)
  const checked = ref(false)
  const message = ref('')
  const outcomes = ref<{ id: string; state: string }[]>([])
  const count = computed(() =>
    drafts.value.reduce((total, row) => total + Object.keys(row.changes).length, 0),
  )

  function stage(
    database: string,
    table: TableRef,
    columns: ColumnRef[],
    result: QueryResult,
    index: number,
    column: string,
    value: string | null,
  ): void {
    if (editUnavailable(table, columns, result)) return
    const metadata = columns.find((field) => field.name === column)
    if (
      !metadata ||
      metadata.generated ||
      (metadata.primaryKeyPosition ?? 0) > 0 ||
      (!metadata.nullable && value === null)
    )
      return
    const original = snapshot(result, index)
    const id = identity(database, table, columns, original)
    let draft = drafts.value.find((row) => row.id === id)
    if (!draft) {
      draft = {
        id,
        keyLabel: columns
          .filter((field) => (field.primaryKeyPosition ?? 0) > 0)
          .map((field) => field.name + '=' + JSON.stringify(original[field.name]))
          .join(', '),
        database,
        schema: table.schema,
        table: table.name,
        original,
        changes: {},
        statement: '',
        error: '',
      }
      drafts.value.push(draft)
      draft = drafts.value[drafts.value.length - 1]
    }
    if (!draft) return
    if (value === draft.original[column]) delete draft.changes[column]
    else draft.changes[column] = value
    draft.statement = ''
    draft.error = ''
    drafts.value = drafts.value.filter((row) => Object.keys(row.changes).length > 0)
    message.value = ''
    checked.value = false
  }

  function overlay(
    database: string,
    table: TableRef,
    columns: ColumnRef[],
    result: QueryResult,
  ): Record<string, string | null> {
    const overlay: Record<string, string | null> = {}
    result.rows.forEach((_, index) => {
      const id = identity(database, table, columns, snapshot(result, index))
      const draft = drafts.value.find((row) => row.id === id)
      if (draft)
        for (const [column, value] of Object.entries(draft.changes))
          overlay[index + ':' + column] = value
    })
    return overlay
  }

  async function review(): Promise<boolean> {
    checked.value = false
    busy.value = true
    message.value = ''
    try {
      for (const row of drafts.value) {
        row.error = ''
        const { data, error, response } = await api.POST('/writes/row-template', {
          body: {
            database: row.database,
            schema: row.schema,
            table: row.table,
            original: row.original,
            changes: row.changes,
          },
        })
        if (!data) {
          row.error = describeFailure(classify(response.status, error))
          return false
        }
        row.statement = data.statement
        const inspected = await api.POST('/writes/inspect', {
          body: { database: row.database, statement: row.statement },
        })
        if (!inspected.data || inspected.data.decision !== 'allow') {
          row.error = inspected.data
            ? inspected.data.reasons.map((reason) => reason.message).join(' ')
            : describeFailure(classify(inspected.response.status, inspected.error))
          return false
        }
      }
      checked.value = true
      return true
    } catch (error) {
      message.value = describeFailure(unreachable(error))
      return false
    } finally {
      busy.value = false
    }
  }

  async function submit(reason: string): Promise<void> {
    if (
      !checked.value ||
      !reason.trim() ||
      drafts.value.some((row) => !row.statement) ||
      busy.value
    )
      return
    busy.value = true
    outcomes.value = []
    const settled = new Set<string>()
    try {
      for (const row of drafts.value) {
        try {
          const { data, error, response } = await api.POST('/writes', {
            body: {
              database: row.database,
              statement: row.statement,
              reason,
              expectedAffectedRows: 1,
            },
          })
          if (!data) {
            row.error = describeFailure(classify(response.status, error))
            continue
          }
          outcomes.value.push({ id: data.id, state: data.state })
          if (data.state === 'applied' || data.state === 'pending') settled.add(row.id)
          else
            row.error =
              data.error ?? 'The write did not apply. Reload the row and review the draft.'
        } catch (error) {
          row.error = describeFailure(unreachable(error))
        }
      }
      drafts.value = drafts.value.filter((row) => !settled.has(row.id))
      const saved = outcomes.value.filter((row) => row.state === 'applied').length
      const pending = outcomes.value.filter((row) => row.state === 'pending').length
      message.value = `${saved} saved, ${pending} awaiting approval, ${drafts.value.length} unsaved.`
      checked.value = false
    } finally {
      busy.value = false
    }
  }

  function discardRow(id: string): void {
    drafts.value = drafts.value.filter((row) => row.id !== id)
    checked.value = false
  }

  function discard(): void {
    checked.value = false
    drafts.value = []
    outcomes.value = []
    message.value = ''
  }
  return {
    drafts,
    busy,
    checked,
    message,
    outcomes,
    count,
    stage,
    overlay,
    review,
    submit,
    discard,
    discardRow,
  }
}
