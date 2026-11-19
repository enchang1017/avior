<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'

import ResultGrid from './ResultGrid.vue'
import { editUnavailable, useRowEdits } from './useRowEdits'
import { cellValue } from './cellValue'
import { VBanner, VButton, VCheckbox, VDialog, VInput, VSelect, VSql, VTable } from '@/components'
import VTextarea from '@/components/Form/VTextarea/VTextarea.vue'
import { lazy } from '@/shared/lazy'
import { useSessionStore } from '@/stores/session'
import type { ColumnRef, QueryResult, TableRef } from '@/shared/api'

const VJsonView = lazy(
  () => import('@/components/Data/VJsonView/VJsonView.vue'),
  'Loading JSON editor',
)
// Content remains the focus; staged values use the existing warning surface,
// with compact shared controls and the native dialog for intentional edits.
const props = defineProps<{
  database: string | null
  table: TableRef | null
  columns: ColumnRef[]
  result: QueryResult | null
  offset: number
  loading: boolean
}>()
const emit = defineEmits<{ saved: [] }>()
const session = useSessionStore()
const edits = useRowEdits()
const enabled = ref(false)
const reviewOpen = ref(false)
const openSQL = ref(new Set<string>())
function toggleSQL(id: string, event: Event): void {
  if ((event.target as HTMLDetailsElement).open) openSQL.value.add(id)
  else openSQL.value.delete(id)
}
const reason = ref('')
const editor = ref<{ row: number; column: string; type: string; nullable: boolean } | null>(null)
const text = ref('')
const jsonValid = ref(true)
type JSONEditorHandle = { currentValue: () => { text: string; valid: boolean } }
let jsonEditor: JSONEditorHandle | null = null
function captureEditor(instance: unknown): void {
  jsonEditor = instance as JSONEditorHandle | null
}
const setNull = ref(false)
const configured = computed(
  () =>
    session.caller?.features?.includes('write') &&
    session.caller.permissions.includes('query.execute'),
)
const unavailable = computed(() =>
  !configured.value
    ? 'Writing is not enabled for this account or deployment.'
    : editUnavailable(props.table, props.columns, props.result),
)
const editable = computed(() =>
  enabled.value && !unavailable.value
    ? props.columns
        .filter((column) => !column.generated && !(column.primaryKeyPosition ?? 0))
        .map((column) => column.name)
    : [],
)
const overlays = computed(() =>
  props.database && props.table && props.result
    ? edits.overlay(props.database, props.table, props.columns, props.result)
    : {},
)
const json = computed(() => /^jsonb?$/iu.test(editor.value?.type ?? ''))
const approval = computed(() => session.caller?.writeApprovalRequired !== false)
const canSubmit = computed(
  () =>
    configured.value &&
    edits.checked.value &&
    reason.value.trim() !== '' &&
    edits.drafts.value.length > 0 &&
    edits.drafts.value.every((row) => row.statement && !row.error) &&
    !edits.busy.value,
)

function begin(row: number, column: number): void {
  if (!props.result || !props.database || !props.table || unavailable.value) return
  const field = props.result.columns[column]
  const metadata = props.columns.find((item) => item.name === field?.name)
  if (!field || !metadata || !editable.value.includes(field.name)) return
  const key = row + ':' + field.name
  const source = Object.prototype.hasOwnProperty.call(overlays.value, key)
    ? overlays.value[key]
    : props.result.rows[row]?.[column]
  text.value = source === null || source === undefined ? '' : cellValue(source, field).raw
  jsonValid.value = true
  setNull.value = source === null
  editor.value = { row, column: field.name, type: metadata.type, nullable: metadata.nullable }
}
function apply(): void {
  if (!editor.value || !props.database || !props.table || !props.result) return
  if (json.value && !setNull.value && jsonEditor) {
    const current = jsonEditor.currentValue()
    if (!current.valid) {
      jsonValid.value = false
      return
    }
    text.value = current.text
  }
  edits.stage(
    props.database,
    props.table,
    props.columns,
    props.result,
    editor.value.row,
    editor.value.column,
    setNull.value ? null : text.value,
  )
  editor.value = null
}
async function review(): Promise<void> {
  reviewOpen.value = true
  await edits.review()
}
async function submit(): Promise<void> {
  if (!canSubmit.value) return
  await edits.submit(reason.value)
  if (edits.drafts.value.length === 0) {
    reviewOpen.value = false
    reason.value = ''
  }
  emit('saved')
}
function discard(): void {
  if (window.confirm('Discard all pending row edits?')) edits.discard()
}
function beforeUnload(event: BeforeUnloadEvent): void {
  if (edits.count.value > 0 || editor.value !== null) event.preventDefault()
}
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
watch(
  () => session.caller?.id,
  (id, before) => {
    if (before && id !== before) {
      edits.discard()
      editor.value = null
      reviewOpen.value = false
    }
  },
)
onBeforeRouteLeave((to) => {
  if (to.path === '/login') {
    edits.discard()
    editor.value = null
    return true
  }
  return (
    (edits.count.value === 0 && editor.value === null) ||
    window.confirm('Leave Query and discard pending row edits?')
  )
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex flex-wrap items-center gap-2 border-b border-line px-3 py-1.5">
      <VButton
        v-if="configured"
        size="sm"
        icon="file-pen"
        :disabled="Boolean(unavailable) || props.loading || edits.busy.value"
        :title="unavailable || 'Double-click a cell or press F2 to edit'"
        @click="enabled = !enabled"
      >
        <span>{{ enabled ? 'Finish editing' : 'Edit rows' }}</span>
      </VButton>
      <span class="text-xs text-fg-muted">{{
        unavailable ||
        (enabled
          ? 'Double-click or F2 to edit. Changes stay pending until reviewed.'
          : 'Read only. Choose Edit rows to stage changes.')
      }}</span>
      <VButton
        v-if="edits.count.value"
        size="sm"
        icon="list-checks"
        :disabled="edits.busy.value"
        @click="review"
      >
        <span>Review {{ edits.count.value }} change(s)</span>
      </VButton>
      <VButton
        v-if="edits.count.value"
        size="sm"
        variant="bare"
        :disabled="edits.busy.value"
        @click="discard"
      >
        <span>Revert all</span>
      </VButton>
    </div>
    <VBanner
      v-if="edits.message.value"
      :tone="edits.drafts.value.some((row) => row.error) ? 'danger' : 'info'"
    >
      <span>{{ edits.message.value }}</span>
    </VBanner>
    <div
      v-if="edits.outcomes.value.length"
      class="flex flex-wrap gap-3 px-3 py-2 text-xs"
      role="status"
    >
      <VButton
        v-for="outcome in edits.outcomes.value"
        :key="outcome.id"
        size="sm"
        variant="bare"
        :to="'/writes?id=' + outcome.id"
      >
        <span
          >{{ outcome.state === 'pending' ? 'Awaiting approval' : outcome.state }} ·
          {{ outcome.id.slice(0, 8) }}</span
        >
      </VButton>
    </div>
    <ResultGrid
      v-if="props.result"
      :result="props.result"
      :animate="false"
      :offset="props.offset"
      :editable-columns="editable"
      :drafts="overlays"
      @edit="begin"
    />
    <VDialog
      :open="editor !== null"
      :title="'Edit ' + (editor?.column ?? 'value')"
      motion="fade"
      @close="editor = null"
    >
      <p class="mt-0 text-xs text-fg-muted">
        {{ editor?.type }} · Applying here stages the value without saving it.
      </p>
      <VCheckbox v-model="setNull" :disabled="!editor?.nullable">Set to NULL</VCheckbox>
      <VJsonView
        v-if="json && !setNull"
        :ref="captureEditor"
        v-model:value="text"
        :read-only="false"
        @valid="jsonValid = $event"
      />
      <VSelect
        v-else-if="editor?.type === 'boolean' && !setNull"
        id="row-edit-boolean"
        v-model="text"
        :options="[
          { value: 'true', label: 'true' },
          { value: 'false', label: 'false' },
        ]"
        aria-label="New value"
      />
      <VTextarea v-else v-model="text" :disabled="setNull" rows="8" aria-label="New value" />
      <template #footer>
        <VButton @click="editor = null">
          <span>Cancel</span>
        </VButton>
        <VButton variant="primary" :disabled="json && !setNull && !jsonValid" @click="apply">
          <span>Apply to draft</span>
        </VButton>
      </template>
    </VDialog>
    <VDialog
      :open="reviewOpen"
      class="leading-5"
      motion="fade"
      title="Review row changes"
      @close="reviewOpen = false"
    >
      <div class="max-h-[calc(var(--spacing-dialog-body-max)-var(--spacing)*32)] overflow-auto">
        <p class="mt-0 text-sm">
          Each row is a separate guarded write.
          {{
            approval ? 'Changes wait for approval.' : 'Changes save immediately after submission.'
          }}
          A stale row fails without overwriting newer data.
        </p>
        <section
          v-for="row in edits.drafts.value"
          :key="row.id"
          class="mb-4 border-b border-line pb-4"
        >
          <h3 class="font-mono text-sm font-normal">
            {{ row.database }} / {{ row.schema }}.{{ row.table }}
          </h3>
          <p class="font-mono text-xs text-fg-muted">Key: {{ row.keyLabel }}</p>
          <VTable>
            <template #caption>Pending values</template>
            <template #head>
              <th scope="col">Column</th>
              <th scope="col">Before</th>
              <th scope="col">After</th>
            </template>
            <tr v-for="(value, column) in row.changes" :key="column">
              <th scope="row" class="font-mono text-sm font-normal">{{ column }}</th>
              <td>
                <pre class="max-h-40 max-w-48 overflow-auto whitespace-pre-wrap">{{
                  row.original[column] === null ? 'NULL' : JSON.stringify(row.original[column])
                }}</pre>
              </td>
              <td>
                <pre class="max-h-40 max-w-48 overflow-auto whitespace-pre-wrap">{{
                  value === null ? 'NULL' : JSON.stringify(value)
                }}</pre>
              </td>
            </tr>
          </VTable>
          <VButton
            size="sm"
            variant="bare"
            :disabled="edits.busy.value"
            @click="edits.discardRow(row.id)"
          >
            <span>Revert this row</span>
          </VButton>
          <p v-if="row.error" class="text-sm text-danger" role="alert">{{ row.error }}</p>
          <details v-if="row.statement" @toggle="toggleSQL(row.id, $event)">
            <summary class="cursor-pointer text-xs">
              Generated SQL · requires exactly one affected row
            </summary>
            <VSql v-if="openSQL.has(row.id)" :code="row.statement" />
          </details>
        </section>
        <p v-if="edits.message.value" class="text-sm" role="status">{{ edits.message.value }}</p>
        <label for="row-edit-reason" class="text-sm">Reason</label>
        <VInput
          id="row-edit-reason"
          v-model="reason"
          maxlength="2000"
          :disabled="edits.busy.value"
        />
      </div>
      <template #footer>
        <VButton :disabled="edits.busy.value" @click="reviewOpen = false">
          <span>Keep editing</span>
        </VButton>
        <VButton :disabled="edits.busy.value" @click="edits.review">
          <span>Check again</span>
        </VButton>
        <VButton variant="primary" :disabled="!canSubmit" @click="submit">
          <span>{{ approval ? 'Submit for approval' : 'Save changes' }}</span>
        </VButton>
      </template>
    </VDialog>
  </div>
</template>
