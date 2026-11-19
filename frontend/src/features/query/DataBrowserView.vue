<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, onBeforeUnmount } from 'vue'

import { rowWorkspace } from './rowWorkspace'
import { lookupRow } from './rowLookup'
import type { RowDraft } from './useRowEdits'
import { lazy } from '@/shared/lazy'
const EditableDataGrid = lazy(() => import('./EditableDataGrid.vue'), 'Loading table rows')
import { VBanner, VButton, VInput, VLoader, VPager, VSelect, VToolbar } from '@/components'
import {
  api,
  classify,
  isGuardVerdict,
  describeFailure,
  describeReason,
  unreachable,
  type ColumnRef,
  type TableRef,
  type Failure,
  type QueryResult,
  type GuardVerdict,
  type Schemas,
} from '@/shared/api'

const VReadout = defineAsyncComponent(() => import('@/components/Data/VReadout/VReadout.vue'))

const bridge = rowWorkspace()
const located = ref<RowDraft | null>(null)
const focusVersion = ref(0)
let controller: AbortController | null = null
onBeforeUnmount(() => {
  generation++
  controller?.abort()
})
watch(bridge.focus, (row) => {
  if (!row) return
  located.value = row
  focusVersion.value++
  bridge.focus.value = null
  void load()
})
watch(bridge.refresh, () => {
  if (ready.value) void load()
})
const props = defineProps<{
  active: boolean
  instanceId: string
  database: string | null
  table: TableRef | null
  columns: ColumnRef[]
  selected: string[]
  metadataBusy: boolean
}>()
const emit = defineEmits<{ sql: [statement: string] }>()
type Operator = 'eq' | 'ne' | 'lt' | 'lte' | 'gt' | 'gte' | 'contains' | 'isNull' | 'isNotNull'
type BrowseState = {
  page: number
  size: string
  column: string
  operator: Operator
  value: string
  order: string
  descending: boolean
}
const states = new Map<string, BrowseState>()
const current = ref<BrowseState>({
  page: 0,
  size: '50',
  column: '',
  operator: 'eq',
  value: '',
  order: '',
  descending: false,
})
const key = computed(() => JSON.stringify([props.database, props.table?.schema, props.table?.name]))
const panel = ref<{
  result: QueryResult | null
  verdict: GuardVerdict | null
  failure: Failure | null
}>({
  result: null,
  verdict: null,
  failure: null,
})
const failure = ref<Failure | null>(null)
const built = ref('')
const applied = ref<Schemas['SelectTemplateRequest'] | null>(null)
const busy = ref(false)
const hasMore = ref(false)
let generation = 0
const options = computed(() =>
  props.columns.filter((c) => !c.denied).map((c) => ({ value: c.name, label: c.name })),
)
const operators = [
  { value: 'eq', label: 'equals' },
  { value: 'ne', label: 'does not equal' },
  { value: 'contains', label: 'contains text' },
  { value: 'lt', label: 'less than' },
  { value: 'lte', label: 'at most' },
  { value: 'gt', label: 'greater than' },
  { value: 'gte', label: 'at least' },
  { value: 'isNull', label: 'is NULL' },
  { value: 'isNotNull', label: 'is not NULL' },
]
const sizeOptions = ['10', '50', '100'].map((value) => ({ value, label: value }))
const nullFilter = computed(() => ['isNull', 'isNotNull'].includes(current.value.operator))
const ready = computed(
  () =>
    props.active &&
    props.database !== null &&
    props.table !== null &&
    props.selected.length > 0 &&
    !props.metadataBusy,
)
const error = computed(() => {
  const problem = failure.value ?? panel.value.failure
  return problem && problem.kind !== 'rejected' ? describeFailure(problem) : null
})
const result = computed<QueryResult | null>(() => {
  const value = panel.value.result
  if (!value) return null
  const rows = value.rows.slice(0, Number(current.value.size))
  return { ...value, rows, rowCount: rows.length }
})

async function load(reset = false): Promise<void> {
  if (located.value && reset) return
  if (bridge.flush && !bridge.flush()) return
  if (reset) current.value.page = 0
  controller?.abort()
  controller = new AbortController()
  const signal = controller.signal
  const token = ++generation
  failure.value = null
  built.value = ''
  hasMore.value = false
  panel.value = { result: null, verdict: null, failure: null }
  if (!ready.value) {
    busy.value = false
    return
  }
  const database = props.database as string
  const table = props.table as TableRef
  const state = { ...current.value }
  states.set(key.value, { ...state })
  busy.value = true
  try {
    const target = located.value
    if (
      target &&
      target.database === database &&
      target.schema === table.schema &&
      target.table === table.name
    ) {
      const row = await lookupRow(target, props.instanceId, signal)
      if (token !== generation) return
      panel.value = { result: row, verdict: null, failure: null }
      return
    }
    const body: Schemas['SelectTemplateRequest'] = {
      instanceId: props.instanceId,
      database,
      schema: table.schema,
      table: table.name,
      columns: props.selected,
      top: Number(state.size) + 1,
      offset: state.page * Number(state.size),
      ...(state.order ? { orderBy: state.order, descending: state.descending } : {}),
      ...(state.column
        ? {
            filter: { column: state.column, operator: state.operator, value: state.value },
          }
        : {}),
    }
    const { data, error: issue, response } = await api.POST('/query/template', { body, signal })
    if (token !== generation) return
    if (!data) {
      failure.value = classify(response.status, issue)
      return
    }
    built.value = data.statement
    applied.value = { ...body, top: Number(state.size) }
    const executed = await api.POST('/query/execute', {
      signal,
      body: {
        instanceId: props.instanceId,
        database,
        statement: data.statement,
        maxRows: Number(state.size) + 1,
      },
    })
    if (token !== generation) return
    if (!executed.data) {
      const problem = classify(executed.response.status, executed.error)
      panel.value = {
        result: null,
        verdict:
          problem.kind === 'rejected'
            ? problem.verdict
            : isGuardVerdict(executed.error)
              ? executed.error
              : null,
        failure: problem,
      }
      return
    }
    panel.value = { result: executed.data, verdict: null, failure: null }
    hasMore.value = executed.data.rows.length > Number(state.size)
  } catch (thrown) {
    if (token === generation) failure.value = unreachable(thrown)
  } finally {
    if (token === generation) busy.value = false
  }
}
watch(
  key,
  () => {
    generation++
    if (
      located.value &&
      key.value !==
        JSON.stringify([located.value.database, located.value.schema, located.value.table])
    )
      located.value = null
    current.value = {
      ...(states.get(key.value) ?? {
        page: 0,
        size: '50',
        column: '',
        operator: 'eq',
        value: '',
        order: '',
        descending: false,
      }),
    }
  },
  { immediate: true, flush: 'sync' },
)
watch(
  () => [props.active, key.value, props.metadataBusy, props.columns, props.selected] as const,
  () => {
    void load()
  },
  { immediate: true },
)
function page(delta: number): void {
  if (located.value || (bridge.flush && !bridge.flush())) return
  if (bridge.flush && !bridge.flush()) return
  current.value.page += delta
  void load()
}
function order(value: string): void {
  if (located.value || (bridge.flush && !bridge.flush())) return
  if (bridge.flush && !bridge.flush()) return
  current.value.order = value
  void load(true)
}
function resize(value: string): void {
  if (located.value || (bridge.flush && !bridge.flush())) return
  if (bridge.flush && !bridge.flush()) return
  current.value.size = value
  void load(true)
}
async function openSQL(): Promise<void> {
  if (!applied.value) return
  const token = generation
  busy.value = true
  try {
    const {
      data,
      error: issue,
      response,
    } = await api.POST('/query/template', { body: applied.value })
    if (token !== generation) return
    if (!data) {
      failure.value = classify(response.status, issue)
      return
    }
    emit('sql', data.statement)
  } catch (thrown) {
    if (token === generation) failure.value = unreachable(thrown)
  } finally {
    if (token === generation) busy.value = false
  }
}

function clear(): void {
  if (located.value || (bridge.flush && !bridge.flush())) return
  current.value.column = ''
  current.value.value = ''
  void load(true)
}
function toggleSort(): void {
  if (located.value || (bridge.flush && !bridge.flush())) return
  current.value.descending = !current.value.descending
  void load(true)
}
function returnToBrowse(): void {
  if (bridge.flush && !bridge.flush()) return
  located.value = null
  void load()
}
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden" aria-label="Table data">
    <VToolbar :busy="busy" busy-label="loading rows">
      <template #lead>
        <span class="font-mono text-sm">{{
          props.table ? props.table.schema + '.' + props.table.name : 'Choose a table'
        }}</span>
        <VButton :disabled="!ready || busy" @click="load()">Refresh</VButton>
        <VButton :disabled="!built || busy" @click="openSQL">Open in SQL</VButton>
      </template>
      <label for="browse-size" class="hud text-fg-muted">Rows</label>
      <div class="w-20">
        <VSelect
          id="browse-size"
          :model-value="current.size"
          :options="sizeOptions"
          :disabled="located !== null || busy"
          @update:model-value="resize"
        />
      </div>
    </VToolbar>
    <form
      class="flex flex-wrap items-center gap-2 border-b border-line p-3"
      @submit.prevent="load(true)"
    >
      <label for="browse-column" class="text-xs text-fg-muted">Filter</label>
      <div class="w-40">
        <VSelect
          id="browse-column"
          v-model="current.column"
          :options="[{ value: '', label: 'No filter' }, ...options]"
          :disabled="located !== null || busy"
        />
      </div>
      <label for="browse-operator" class="sr-only">Filter operator</label>
      <div class="w-40">
        <VSelect
          id="browse-operator"
          v-model="current.operator"
          aria-label="Filter operator"
          :options="operators"
          :disabled="located !== null || busy || !current.column"
        />
      </div>
      <div class="w-40">
        <VInput
          v-model="current.value"
          aria-label="Filter value"
          placeholder="Value"
          :disabled="located !== null || busy || !current.column || nullFilter"
          maxlength="4096"
        />
      </div>
      <VButton type="submit" :disabled="!ready || busy || located !== null">Apply</VButton>
      <VButton :disabled="!ready || busy || located !== null" @click="clear">Clear</VButton>
      <label for="browse-order" class="text-xs text-fg-muted">Sort</label>
      <div class="w-40">
        <VSelect
          id="browse-order"
          :model-value="current.order"
          :options="[{ value: '', label: 'Stable default' }, ...options]"
          :disabled="located !== null || busy"
          @update:model-value="order"
        />
      </div>
      <VButton :disabled="located !== null || busy || !current.order" @click="toggleSort">
        {{ current.descending ? 'DESC' : 'ASC' }}
      </VButton>
    </form>
    <VBanner v-if="error" icon="circle-x">{{ error }}</VBanner>
    <VBanner v-if="panel.verdict?.decision === 'deny'" icon="circle-x">
      <p v-for="reason in panel.verdict.reasons" :key="reason.rule" class="m-0">
        {{ describeReason(reason) }}
      </p>
    </VBanner>
    <div v-if="busy" class="flex justify-center py-10">
      <VLoader size="section" label="Loading table rows" />
    </div>
    <template v-else-if="result">
      <VReadout
        :parts="[
          { value: 'ALLOWED', tone: 'ok' },
          { key: 'LOG', value: result.auditId },
          { key: 'ELAPSED', value: result.elapsedMs + ' MS' },
        ]"
        label="Data query audit"
      />
    </template>
    <p v-else-if="!error && !panel.verdict" class="m-0 p-4 text-sm text-fg-muted">
      Choose a table and at least one readable column to browse its rows.
    </p>
    <VBanner v-if="located" tone="info">
      <span
        >Showing requested row: {{ located.keyLabel }}.
        {{ result?.rows.length === 0 ? 'The row no longer exists or is not readable.' : '' }}</span
      >
      <VButton size="sm" variant="bare" @click="returnToBrowse">
        <span>Return to saved page and filter</span>
      </VButton>
    </VBanner>
    <EditableDataGrid
      :database="props.database"
      :table="props.table"
      :columns="props.columns"
      :result="result"
      :offset="located ? 0 : current.page * Number(current.size)"
      :focus-row="located ? focusVersion : 0"
      :loading="busy"
    />
    <div class="mt-auto">
      <VPager
        :page="located ? 1 : current.page + 1"
        :can-go-back="!located && current.page > 0"
        :has-more="!located && hasMore && (current.page + 1) * Number(current.size) <= 1000000"
        :busy="busy || located !== null"
        @previous="page(-1)"
        @next="page(1)"
      />
      <p class="m-0 px-4 py-2 text-xs text-fg-muted">
        Each page is a guarded, audited SELECT. Changes to the table may move rows between pages.
      </p>
    </div>
  </section>
</template>
