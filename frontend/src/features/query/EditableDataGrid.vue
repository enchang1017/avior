<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch, nextTick } from 'vue'

import ResultGrid from './ResultGrid.vue'
import { editUnavailable, useWorkspaceRowEdits } from './useRowEdits'
import { inlineCell, validateCell, cellKind, cellHint } from './cellEditing'
import { cellValue } from './cellValue'
import { VBanner, VButton, VCheckbox, VDialog, VInput, VSelect } from '@/components'
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
  focusRow?: number
}>()

const grid = ref<HTMLElement | null>(null)
let focused = 0
watch(
  () => [props.focusRow, props.result] as const,
  async () => {
    if (!props.focusRow || props.focusRow === focused || !props.result?.rows.length) return
    await nextTick()
    const cell = grid.value?.querySelector<HTMLElement>('.result tr[data-row] td')
    if (cell) {
      focused = props.focusRow
      cell.focus()
      cell.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    }
  },
  { flush: 'post' },
)
const session = useSessionStore()
const { edits, bridge } = useWorkspaceRowEdits()
const enabled = ref(false)
const inlineEditor = ref<{
  row: number
  column: string
  value: string | null
  error: string
  nullable: boolean
} | null>(null)
type EditContext = {
  database: string
  table: TableRef
  columns: ColumnRef[]
  result: QueryResult
  row: number
  column: ColumnRef
}
let editingContext: EditContext | null = null
const fieldError = ref('')
const editor = ref<ColumnRef | null>(null)
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
    ? 'Row editing requires the write feature and query execution permission. Ask an administrator to enable the writer for this deployment and review your account access.'
    : editUnavailable(props.table, props.columns, props.result),
)
const editable = computed(() =>
  enabled.value && !unavailable.value && !edits.busy.value
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
const json = computed(() => editor.value !== null && cellKind(editor.value) === 'json')
const blocked = computed(() =>
  props.database && props.table && props.result
    ? edits.blockedRows(props.database, props.table, props.columns, props.result)
    : {},
)
const readOnlyReasons = computed(() =>
  Object.fromEntries(
    props.columns
      .filter((column) => column.generated || (column.primaryKeyPosition ?? 0) > 0)
      .map((column) => [
        column.name,
        column.generated
          ? 'Generated and identity values are maintained by PostgreSQL.'
          : 'Primary keys identify staged rows and are read-only.',
      ]),
  ),
)
function flush(): boolean {
  if (inlineEditor.value) return applyInline()
  return editor.value === null
}
bridge.flush = flush
function stage(value: string | null): boolean {
  const context = editingContext
  if (!context) return false
  const invalid = validateCell(context.column, value)
  if (inlineEditor.value) inlineEditor.value.error = invalid
  fieldError.value = invalid
  if (invalid) return false
  edits.stage(
    context.database,
    context.table,
    context.columns,
    context.result,
    context.row,
    context.column.name,
    value,
  )
  return true
}
function applyInline(): boolean {
  if (!inlineEditor.value) return true
  if (!stage(inlineEditor.value.value)) return false
  inlineEditor.value = null
  editingContext = null
  return true
}
function cancelInline(): void {
  inlineEditor.value = null
  editingContext = null
  fieldError.value = ''
}

function begin(row: number, column: number): void {
  if (
    !flush() ||
    !props.result ||
    !props.database ||
    !props.table ||
    unavailable.value ||
    blocked.value[row]
  )
    return
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
  editingContext = {
    database: props.database,
    table: { ...props.table },
    columns: props.columns.map((column) => ({ ...column })),
    result: props.result,
    row,
    column: metadata,
  }
  fieldError.value = ''
  if (inlineCell(metadata, source === null ? null : text.value)) {
    inlineEditor.value = {
      row,
      column: field.name,
      value: source === null ? null : text.value,
      error: '',
      nullable: metadata.nullable,
    }
    return
  }
  editor.value = { ...metadata }
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
  if (!stage(setNull.value ? null : text.value)) return
  editor.value = null
  editingContext = null
}
function review(): void {
  if (flush()) bridge.open.value = true
}

function discard(): void {
  if (window.confirm('Discard all pending row edits?')) edits.discard()
}
watch(
  [inlineEditor, editor],
  () => {
    bridge.editing.value = inlineEditor.value !== null || editor.value !== null
  },
  { flush: 'sync' },
)
onBeforeUnmount(() => {
  if (bridge.flush === flush) bridge.flush = null
  bridge.editing.value = false
})
watch(
  () => session.caller?.id,
  (id, before) => {
    if (before && id !== before) {
      edits.reset()
      editor.value = null
      cancelInline()
      bridge.open.value = false
    }
  },
)
</script>

<template>
  <div ref="grid" class="flex min-h-0 flex-1 flex-col">
    <div class="flex flex-wrap items-center gap-2 border-b border-line px-3 py-1.5">
      <VButton
        v-if="configured"
        size="sm"
        icon="file-pen"
        :disabled="Boolean(unavailable) || props.loading || edits.busy.value"
        :title="unavailable || 'Double-click a cell or press F2 to edit'"
        @click="flush() && (enabled = !enabled)"
      >
        <span>{{ enabled ? 'Finish editing' : 'Edit rows' }}</span>
      </VButton>
      <span class="text-xs text-fg-muted">{{
        unavailable ||
        (enabled
          ? 'Double-click or F2 to edit. Enter stages, Esc cancels, Tab moves cells, and Alt+Enter toggles NULL.'
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
    <p v-if="inlineEditor?.error" class="m-0 px-3 py-2 text-xs text-danger" role="alert">
      {{ inlineEditor.error }}
    </p>
    <p v-if="!configured" class="m-0 px-3 pb-2 text-xs">
      <a
        class="text-primary underline"
        href="https://github.com/enchang1017/avior/blob/master/docs/runbooks/local-sidecar.md#edit-existing-rows"
        target="_blank"
        rel="noopener noreferrer"
        >Set up governed writer credentials</a
      >
    </p>
    <ResultGrid
      v-if="props.result"
      :result="props.result"
      :animate="false"
      :offset="props.offset"
      :editable-columns="editable"
      :drafts="overlays"
      :inline-editor="inlineEditor"
      :blocked-rows="blocked"
      :read-only-reasons="readOnlyReasons"
      @inline-value="inlineEditor && ((inlineEditor.value = $event), (inlineEditor.error = ''))"
      @inline-save="applyInline"
      @inline-cancel="cancelInline"
      @edit="begin"
    />
    <VDialog
      :open="editor !== null"
      :title="'Edit ' + (editor?.name ?? 'value')"
      motion="fade"
      @close="editor = null"
    >
      <p class="mt-0 text-xs text-fg-muted">
        {{ editor?.type }} · Applying here stages the value without saving it.
        {{ editor ? cellHint(editor) : '' }}
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
        v-else-if="editor?.enumValues?.length && !setNull"
        id="row-edit-enum"
        v-model="text"
        :options="editor.enumValues.map((value) => ({ value, label: JSON.stringify(value) }))"
        aria-label="New value"
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
      <VInput
        v-else-if="
          editor && ['date', 'time', 'timestamp', 'number'].includes(cellKind(editor)) && !setNull
        "
        v-model="text"
        :type="
          cellKind(editor) === 'date' && (text === '' || /^\d{4}-\d{2}-\d{2}$/u.test(text))
            ? 'date'
            : 'text'
        "
        aria-label="New value"
      />
      <VTextarea v-else v-model="text" :disabled="setNull" rows="8" aria-label="New value" />
      <p v-if="fieldError" class="text-sm text-danger" role="alert">{{ fieldError }}</p>
      <template #footer>
        <VButton @click="editor = null">
          <span>Cancel</span>
        </VButton>
        <VButton variant="primary" :disabled="json && !setNull && !jsonValid" @click="apply">
          <span>Apply to draft</span>
        </VButton>
      </template>
    </VDialog>
  </div>
</template>
