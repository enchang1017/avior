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
import { rowSnapshot, lookupRow, rowError } from './rowLookup'
import { validateCell } from './cellEditing'
import { rowWorkspace } from './rowWorkspace'

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
  columns: ColumnRef[]
  latest?: Record<string, string | null> | null
  choices: Record<string, 'mine' | 'current' | ''>
}
export type WriteOutcome = {
  id: string
  state: string
  row: RowDraft
  error: string
  restored: boolean
  notice: string
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
  if (!table || table.kind !== 'table')
    return 'Only ordinary tables can be edited. Browse this source or open its SELECT in SQL.'
  if (columns.length === 0 || !columns.some((column) => (column.primaryKeyPosition ?? 0) > 0))
    return 'This table has no readable primary key. Ask the table owner to define a stable key before editing rows.'
  if (
    columns.some(
      (column) => (column.primaryKeyPosition ?? 0) > 0 && column.generated && !column.identity,
    )
  )
    return 'Tables with expression-generated primary keys are read-only.'
  if (columns.some((column) => column.denied) || result?.columns.some((column) => column.masked))
    return (
      'Restricted columns make this table read-only: ' +
      columns
        .filter((column) => column.denied)
        .map((column) => column.name)
        .join(', ') +
      '. Ask an administrator to review your access; changing editors does not bypass permissions.'
    )
  if (columns.some((column) => !supportedCellType(column.type) && !column.enumValues?.length))
    return (
      'Unsupported columns: ' +
      columns
        .filter((column) => !supportedCellType(column.type) && !column.enumValues?.length)
        .map((column) => column.name + ' (' + column.type + ')')
        .join(', ') +
      '. Inspect these values or use the governed SQL write workflow.'
    )
  if (
    !result ||
    result.columns.length !== columns.length ||
    columns.some((column) => !result.columns.some((field) => field.name === column.name))
  )
    return 'Select all columns to edit a complete row.'
  return ''
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
export function useRowEdits(instanceId = '') {
  let generation = 0
  const controllers = new Set<AbortController>()
  function scope() {
    const controller = new AbortController()
    controllers.add(controller)
    return { generation, controller }
  }
  function active(operation: ReturnType<typeof scope>): boolean {
    return operation.generation === generation
  }
  function finish(operation: ReturnType<typeof scope>): void {
    controllers.delete(operation.controller)
  }
  function cancelRequests(): void {
    generation++
    for (const controller of controllers) controller.abort()
    controllers.clear()
    busy.value = false
    refreshing.value = false
  }
  const drafts = ref<RowDraft[]>([])
  const busy = ref(false)
  const checked = ref(false)
  const message = ref('')
  const outcomes = ref<WriteOutcome[]>([])
  const appliedVersion = ref(0)
  const refreshing = ref(false)
  const trackingError = ref('')
  const readyRows = computed(() =>
    drafts.value.filter((row) => row.statement && !row.error && row.latest === undefined),
  )
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
    const original = rowSnapshot(result, index)
    const id = identity(database, table, columns, original)
    if (outcomes.value.some((item) => item.row.id === id && item.state === 'pending')) {
      message.value =
        'This row is awaiting approval. Refresh its request status before editing again.'
      return
    }
    const invalid = validateCell(metadata, value)
    if (invalid) {
      message.value = invalid
      return
    }
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
        columns: columns.map((column) => ({ ...column })),
        choices: {},
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
      const id = identity(database, table, columns, rowSnapshot(result, index))
      const draft = drafts.value.find((row) => row.id === id)
      if (draft)
        for (const [column, value] of Object.entries(draft.changes))
          overlay[index + ':' + column] = value
    })
    return overlay
  }

  async function review(): Promise<boolean> {
    if (busy.value) return false
    const operation = scope()
    checked.value = false
    busy.value = true
    message.value = ''
    try {
      for (const row of [...drafts.value]) {
        row.statement = ''
        row.error = ''
        try {
          await refreshRow(row)
          if (!active(operation)) return false
          if (row.latest !== undefined) continue
          const built = await api.POST('/writes/row-template', {
            signal: operation.controller.signal,
            body: {
              database: row.database,
              schema: row.schema,
              table: row.table,
              original: row.original,
              changes: row.changes,
            },
          })
          if (!active(operation)) return false
          if (!built.data) {
            row.error = describeFailure(classify(built.response.status, built.error))
            continue
          }
          const inspected = await api.POST('/writes/inspect', {
            signal: operation.controller.signal,
            body: { database: row.database, statement: built.data.statement },
          })
          if (!active(operation)) return false
          if (!inspected.data || inspected.data.decision !== 'allow') {
            row.error = inspected.data
              ? inspected.data.reasons.map((reason) => reason.message).join(' ')
              : describeFailure(classify(inspected.response.status, inspected.error))
            continue
          }
          row.statement = built.data.statement
        } catch (error) {
          if (!active(operation)) return false
          row.error = rowError(error)
        }
      }
      checked.value = true
      return drafts.value.every((row) => !row.error)
    } finally {
      finish(operation)
      if (active(operation)) busy.value = false
    }
  }

  async function refreshRow(row: RowDraft): Promise<void> {
    const operation = scope()
    try {
      const result = await lookupRow(row, instanceId, operation.controller.signal)
      if (!active(operation)) return
      if (result.rows.length === 0) {
        row.latest = null
        row.error = 'This row was deleted. Copy your draft or discard its changes.'
        return
      }
      const latest = rowSnapshot(result, 0)
      if (
        Object.keys(latest).length !== Object.keys(row.original).length ||
        Object.keys(row.original).some((key) => !(key in latest))
      )
        throw new Error(
          'The table columns changed. Reload its metadata and keep a copy of this draft before recreating the edit.',
        )
      if (Object.keys(latest).every((key) => latest[key] === row.original[key])) {
        delete row.latest
        return
      }
      row.latest = latest
      row.statement = ''
      row.choices = {}
      for (const column of Object.keys(row.changes))
        row.choices[column] =
          latest[column] === row.original[column] || latest[column] === row.changes[column]
            ? 'mine'
            : ''
      row.error =
        'This row changed in the database. Review the current values and apply your choices.'
    } finally {
      finish(operation)
    }
  }
  function rebase(row: RowDraft): boolean {
    if (!row.latest) return false
    if (Object.keys(row.changes).some((column) => !row.choices[column])) {
      row.error = 'Choose Keep mine or Use current for each edited field.'
      return false
    }
    const changes = Object.fromEntries(
      Object.entries(row.changes).filter(
        ([column, value]) => row.choices[column] === 'mine' && value !== row.latest?.[column],
      ),
    )
    row.original = { ...row.latest }
    row.changes = changes
    delete row.latest
    row.choices = {}
    row.statement = ''
    row.error = ''
    checked.value = false
    if (!Object.keys(changes).length) discardRow(row.id)
    message.value = 'Latest values adopted. Check the remaining changes again before submitting.'
    return true
  }

  async function submit(reason: string): Promise<void> {
    if (!checked.value || !reason.trim() || readyRows.value.length === 0 || busy.value) return
    const operation = scope()
    busy.value = true
    const submitted: WriteOutcome[] = []
    const settled = new Set<string>()
    try {
      for (const row of [...readyRows.value]) {
        if (!active(operation)) return
        try {
          const { data, error, response } = await api.POST('/writes', {
            signal: operation.controller.signal,
            body: {
              database: row.database,
              statement: row.statement,
              reason,
              expectedAffectedRows: 1,
            },
          })
          if (!active(operation)) return
          if (!data) {
            row.error = describeFailure(classify(response.status, error))
            continue
          }
          const outcome: WriteOutcome = {
            id: data.id,
            state: data.state,
            row: clone(row),
            error: data.error ?? '',
            restored: !['applied', 'pending'].includes(data.state),
            notice: '',
          }
          outcomes.value.push(outcome)
          submitted.push(outcome)
          if (data.state === 'applied' || data.state === 'pending') settled.add(row.id)
          else
            row.error =
              data.error ?? 'The write did not apply. Reload the row and review the draft.'
        } catch (error) {
          if (!active(operation)) return
          row.error = describeFailure(unreachable(error))
        }
      }
      if (!active(operation)) return
      drafts.value = drafts.value.filter((row) => !settled.has(row.id))
      const saved = submitted.filter((row) => row.state === 'applied').length
      const pending = submitted.filter((row) => row.state === 'pending').length
      message.value = [
        saved ? `${saved} saved` : '',
        pending ? `${pending} submitted for approval` : '',
        drafts.value.length ? `${drafts.value.length} unsaved` : '',
      ]
        .filter(Boolean)
        .join(' · ')
      checked.value = false
    } finally {
      finish(operation)
      if (active(operation)) busy.value = false
    }
  }

  function discardRow(id: string): void {
    if (busy.value) return
    drafts.value = drafts.value.filter((row) => row.id !== id)
    checked.value = false
  }

  function discard(): void {
    cancelRequests()
    checked.value = false
    drafts.value = []
    message.value = ''
  }
  function clone(row: RowDraft): RowDraft {
    return {
      ...row,
      original: { ...row.original },
      changes: { ...row.changes },
      columns: row.columns.map((column) => ({ ...column })),
      choices: { ...row.choices },
    }
  }
  function restore(outcome: WriteOutcome): void {
    if (drafts.value.some((row) => row.id === outcome.row.id)) {
      outcome.restored = true
      outcome.notice =
        'A newer local draft exists for this row. It was kept; copy this submitted draft to compare manually.'
      return
    }
    const row = clone(outcome.row)
    row.statement = ''
    row.error =
      outcome.error || 'This request did not apply. Check the current row before resubmitting.'
    delete row.latest
    drafts.value.push(row)
    outcome.restored = true
    checked.value = false
  }
  async function refreshOutcomes(): Promise<void> {
    if (refreshing.value || busy.value) return
    const operation = scope()
    refreshing.value = true
    trackingError.value = ''
    try {
      for (const outcome of [...outcomes.value]) {
        try {
          const result = await api.GET('/writes/{writeId}', {
            signal: operation.controller.signal,
            params: { path: { writeId: outcome.id } },
          })
          if (!active(operation)) return
          if (!result.data) {
            trackingError.value = describeFailure(classify(result.response.status, result.error))
            continue
          }
          const before = outcome.state
          outcome.state = result.data.request.rolledBackBy ? 'undone' : result.data.request.state
          if (before !== outcome.state && ['applied', 'undone'].includes(outcome.state))
            appliedVersion.value++
          outcome.error = result.data.request.error ?? result.data.request.decisionNote ?? ''
          if (['failed', 'rejected', 'withdrawn'].includes(outcome.state) && !outcome.restored)
            restore(outcome)
        } catch (error) {
          if (!active(operation)) return
          trackingError.value = rowError(error)
        }
      }
    } finally {
      finish(operation)
      if (active(operation)) refreshing.value = false
    }
  }
  function blockedRows(
    database: string,
    table: TableRef,
    columns: ColumnRef[],
    result: QueryResult,
  ): Record<number, string> {
    const blocked: Record<number, string> = {}
    result.rows.forEach((_, index) => {
      const id = identity(database, table, columns, rowSnapshot(result, index))
      const request = outcomes.value.find(
        (outcome) => outcome.row.id === id && outcome.state === 'pending',
      )
      if (request)
        blocked[index] =
          'Awaiting approval (' +
          request.id.slice(0, 8) +
          '). Refresh request status before editing this row again.'
    })
    return blocked
  }
  function reset(): void {
    discard()
    outcomes.value = []
    trackingError.value = ''
  }
  return {
    appliedVersion,
    readyRows,
    refreshRow,
    rebase,
    refreshOutcomes,
    refreshing,
    trackingError,
    restore,
    blockedRows,
    reset,
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

export function useWorkspaceRowEdits() {
  const bridge = rowWorkspace()
  bridge.state.value ??= useRowEdits(bridge.instanceId)
  return { bridge, edits: bridge.state.value }
}
