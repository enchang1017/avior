<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import {
  VBanner,
  VButton,
  VDialog,
  VIcon,
  VInput,
  VSelect,
  VSql,
  VStatusIcon,
  VTable,
} from '@/components'
import { useSessionStore } from '@/stores/session'
import { useWorkspaceRowEdits, type RowDraft, type WriteOutcome } from './useRowEdits'

// A developer's change ledger: row keys and differences lead, existing neutral
// surfaces and mono type preserve precision, and four-pixel spacing stays dense.
const { bridge, edits } = useWorkspaceRowEdits()
const session = useSessionStore()
const reason = ref('')
const openSQL = ref(new Set<string>())
const rowTools = ref(new Set<string>())
const optionsOpen = ref(false)
const historyOpen = ref(false)
const notApplied = computed(
  () =>
    edits.outcomes.value.filter((outcome) =>
      ['failed', 'rejected', 'withdrawn'].includes(outcome.state),
    ).length,
)
const followup = computed(
  () =>
    edits.outcomes.value.filter(
      (outcome) => ['failed', 'rejected', 'withdrawn'].includes(outcome.state) && !outcome.restored,
    ).length,
)
const attention = computed(() => edits.drafts.value.filter((row) => row.error).length)
const retryable = computed(() =>
  edits.drafts.value.some((row) => row.error && row.latest === undefined),
)
const submitLabel = computed(() => {
  const count = edits.readyRows.value.length
  if (!count) return approval.value ? 'Submit for approval' : 'Save changes'
  const rows = `${count} ${attention.value ? 'ready ' : ''}${count === 1 ? 'row' : 'rows'}`
  return approval.value ? `Submit ${rows} for approval` : `Save ${rows}`
})
const approval = computed(() => session.caller?.writeApprovalRequired !== false)
const pending = computed(
  () => edits.outcomes.value.filter((item) => item.state === 'pending').length,
)
const groups = computed(() => {
  const grouped = new Map<string, { label: string; rows: RowDraft[] }>()
  for (const row of edits.drafts.value) {
    const key = JSON.stringify([row.database, row.schema, row.table])
    if (!grouped.has(key))
      grouped.set(key, { label: row.database + ' / ' + row.schema + '.' + row.table, rows: [] })
    grouped.get(key)?.rows.push(row)
  }
  return [...grouped.values()]
})
const canSubmit = computed(
  () =>
    session.caller?.features.includes('write') &&
    session.caller.permissions.includes('query.execute') &&
    edits.checked.value &&
    edits.readyRows.value.length > 0 &&
    reason.value.trim() !== '' &&
    !edits.busy.value,
)
function fields(row: RowDraft): string[] {
  return Object.keys(row.original).filter(
    (column) =>
      column in row.changes || (row.latest && row.latest[column] !== row.original[column]),
  )
}
function toggleTools(id: string): void {
  if (rowTools.value.has(id)) {
    rowTools.value.delete(id)
    openSQL.value.delete(id)
  } else rowTools.value.add(id)
}
function discardAll(): void {
  if (window.confirm('Discard all unsaved changes?')) edits.discard()
}
function outcomeLabel(state: string): string {
  return state === 'pending' ? 'Awaiting approval' : state.charAt(0).toUpperCase() + state.slice(1)
}
function outcomeStatus(state: string): 'ok' | 'pending' | 'refused' | 'idle' {
  if (state === 'applied') return 'ok'
  if (state === 'pending') return 'pending'
  return ['failed', 'rejected'].includes(state) ? 'refused' : 'idle'
}
function display(value: string | null | undefined): string {
  return value === null ? 'NULL' : value === undefined ? '—' : JSON.stringify(value)
}
async function open(): Promise<void> {
  if (bridge.flush && !bridge.flush()) return
  if (!edits.count.value && !edits.outcomes.value.length) return
  edits.message.value = ''
  optionsOpen.value = false
  rowTools.value.clear()
  openSQL.value.clear()
  historyOpen.value = !edits.drafts.value.length
  bridge.open.value = true
  if (edits.drafts.value.length) await edits.review()
  await edits.refreshOutcomes()
}
watch(edits.count, (count, before) => {
  if (before && !count) {
    bridge.open.value = false
    reason.value = ''
  }
})
watch(bridge.open, (value, before) => {
  if (value && !before && !edits.busy.value && edits.drafts.value.length) void edits.review()
})
async function recheck(row: RowDraft): Promise<void> {
  if (edits.rebase(row)) await edits.review()
}
async function submit(): Promise<void> {
  if (!canSubmit.value) return
  await edits.submit(reason.value)
  bridge.refresh.value++
  if (!edits.drafts.value.length) {
    bridge.open.value = false
    reason.value = ''
  }
}
async function navigate(row: RowDraft): Promise<void> {
  bridge.open.value = false
  await bridge.navigate?.(row)
}
function toggleSQL(id: string): void {
  if (openSQL.value.has(id)) openSQL.value.delete(id)
  else openSQL.value.add(id)
}
async function copy(row: RowDraft): Promise<void> {
  try {
    await navigator.clipboard.writeText(
      JSON.stringify(
        {
          database: row.database,
          schema: row.schema,
          table: row.table,
          original: row.original,
          changes: row.changes,
        },
        null,
        2,
      ),
    )
    edits.message.value = 'Draft copied.'
  } catch {
    edits.message.value = 'Clipboard unavailable. Select and copy the draft values.'
  }
}
function restore(outcome: WriteOutcome): void {
  edits.restore(outcome)
}
function beforeUnload(event: BeforeUnloadEvent): void {
  if (edits.count.value || bridge.editing.value) event.preventDefault()
}
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
onBeforeRouteLeave((to) => {
  if (to.path === '/login') {
    edits.reset()
    return true
  }
  if (!edits.count.value && !bridge.editing.value) return true
  if (!window.confirm('Leave Query and discard pending row edits?')) return false
  edits.discard()
  return true
})
watch(
  () => session.caller?.id,
  (id, before) => {
    if (before && id !== before) edits.reset()
  },
  { immediate: true },
)
watch(edits.appliedVersion, () => {
  if (!bridge.editing.value) bridge.refresh.value++
})
let timer: ReturnType<typeof setInterval> | undefined
function refreshVisible(): void {
  if (!document.hidden && pending.value && !edits.busy.value) void edits.refreshOutcomes()
}
onMounted(() => {
  timer = setInterval(refreshVisible, 15000)
  document.addEventListener('visibilitychange', refreshVisible)
  refreshVisible()
})
onBeforeUnmount(() => {
  clearInterval(timer)
  document.removeEventListener('visibilitychange', refreshVisible)
})
</script>
<template>
  <VButton
    size="sm"
    :icon="attention || followup ? 'warning' : pending ? 'clock' : 'list-checks'"
    :disabled="!edits.count.value && !edits.outcomes.value.length && !bridge.editing.value"
    @click="open"
  >
    <span :class="attention || followup ? 'text-warn' : pending ? 'text-target' : ''"
      >Changes{{ edits.count.value ? ' (' + edits.count.value + ')' : ''
      }}{{
        attention || followup
          ? ' · ' + (attention + followup) + ' need attention'
          : pending
            ? ' · ' + pending + ' pending'
            : edits.outcomes.value.length
              ? ' · ' + edits.outcomes.value.length + ' submitted'
              : ''
      }}</span
    >
  </VButton>
  <VDialog
    :open="bridge.open.value"
    class="leading-5"
    motion="fade"
    title="Changes"
    @close="bridge.open.value = false"
  >
    <div>
      <div
        v-if="edits.busy.value || attention"
        class="mb-3 flex items-center gap-2 text-xs"
        role="status"
      >
        <VStatusIcon :status="edits.busy.value ? 'running' : 'warn'" size="sm" />
        <span :class="attention ? 'text-warn' : 'text-fg-muted'">{{
          edits.busy.value ? 'Checking…' : attention + ' need attention'
        }}</span>
      </div>
      <div v-if="optionsOpen" class="mb-3 border-b border-line pb-3">
        <p class="mt-0 text-xs text-fg-muted">
          Unsaved changes stay in this tab until reload or sign-out.
        </p>
        <VButton
          v-if="edits.drafts.value.length"
          size="sm"
          variant="bare"
          danger
          :disabled="edits.busy.value"
          @click="discardAll"
        >
          <span>Discard all changes</span>
        </VButton>
      </div>
      <section v-for="group in groups" :key="group.label" class="mb-4 border-b border-line pb-4">
        <h3 class="font-mono text-sm font-normal">{{ group.label }}</h3>
        <section v-for="row in group.rows" :key="row.id" class="mb-4">
          <div class="mb-1 flex items-center justify-between gap-2">
            <p class="m-0 min-w-0 font-mono text-xs wrap-anywhere text-fg-muted">
              {{ row.keyLabel }}
            </p>
            <div class="flex shrink-0 items-center gap-1">
              <VButton
                size="sm"
                variant="bare"
                icon="crosshair"
                :disabled="edits.busy.value"
                @click="navigate(row)"
              >
                <span>Show row</span>
              </VButton>
              <VButton
                size="sm"
                variant="bare"
                :aria-expanded="rowTools.has(row.id)"
                :aria-label="'More actions for ' + row.keyLabel"
                title="More row actions"
                @click="toggleTools(row.id)"
              >
                <span aria-hidden="true">⋯</span>
              </VButton>
            </div>
          </div>
          <div class="overflow-auto">
            <VTable class="table-fixed">
              <template #caption>Pending values</template>
              <template #head>
                <th scope="col" class="w-20">Column</th>
                <th scope="col">Before</th>
                <th v-if="row.latest" scope="col" class="whitespace-normal">Current database</th>
                <th scope="col" class="text-primary">{{ row.latest ? 'Draft' : 'After' }}</th>
                <th v-if="row.latest" scope="col" class="w-40">Resolution</th>
              </template>
              <tr v-for="column in fields(row)" :key="column">
                <th scope="row" class="font-mono text-sm font-normal wrap-anywhere">
                  {{ column }}
                </th>
                <td class="text-fg-muted">
                  <pre
                    class="max-h-40 max-w-full overflow-auto wrap-anywhere whitespace-pre-wrap"
                    >{{ display(row.original[column]) }}</pre
                  >
                </td>
                <td v-if="row.latest">
                  <pre
                    class="max-h-40 max-w-full overflow-auto wrap-anywhere whitespace-pre-wrap"
                    >{{ display(row.latest[column]) }}</pre
                  >
                </td>
                <td>
                  <pre
                    class="max-h-40 max-w-full overflow-auto wrap-anywhere whitespace-pre-wrap"
                    >{{ column in row.changes ? display(row.changes[column]) : '—' }}</pre
                  >
                </td>
                <td v-if="row.latest">
                  <VSelect
                    v-if="column in row.changes"
                    :id="'resolve-' + row.id + '-' + column"
                    :model-value="row.choices[column] ?? ''"
                    :aria-label="'Resolve ' + column"
                    :options="[
                      { value: '', label: 'Choose' },
                      { value: 'mine', label: 'Keep mine' },
                      { value: 'current', label: 'Use current' },
                    ]"
                    @update:model-value="row.choices[column] = $event as 'mine' | 'current' | ''"
                  />
                  <span v-else class="text-xs text-fg-muted">Use current</span>
                </td>
              </tr>
            </VTable>
          </div>
          <div
            v-if="rowTools.has(row.id)"
            class="flex flex-wrap gap-2 py-2"
            role="group"
            :aria-label="'Actions for ' + row.keyLabel"
          >
            <VButton
              size="sm"
              variant="bare"
              :disabled="edits.busy.value"
              @click="edits.discardRow(row.id)"
            >
              <span>Discard row changes</span>
            </VButton>
            <VButton size="sm" variant="bare" icon="copy" @click="copy(row)">
              <span>Copy draft</span>
            </VButton>
            <VButton
              v-if="row.statement"
              size="sm"
              variant="bare"
              icon="file-code"
              :aria-expanded="openSQL.has(row.id)"
              @click="toggleSQL(row.id)"
            >
              <span>SQL</span>
            </VButton>
          </div>
          <p
            v-if="row.error"
            class="flex items-start gap-2 text-sm"
            :class="row.latest ? 'text-warn' : 'text-danger'"
            role="alert"
          >
            <VStatusIcon :status="row.latest ? 'warn' : 'refused'" size="sm" /><span>{{
              row.error
            }}</span>
          </p>
          <div v-if="row.latest !== undefined || row.error" class="flex flex-wrap gap-2 py-2">
            <VButton v-if="row.latest" size="sm" :disabled="edits.busy.value" @click="recheck(row)">
              <span>Apply choices</span>
            </VButton>
            <VButton
              v-if="row.error && !row.latest && !rowTools.has(row.id)"
              size="sm"
              variant="bare"
              icon="copy"
              @click="copy(row)"
            >
              <span>Copy draft</span>
            </VButton>
          </div>
          <VSql
            v-if="row.statement && bridge.open.value && openSQL.has(row.id)"
            :code="row.statement"
          />
        </section>
      </section>
      <VBanner v-if="edits.message.value" tone="info">
        <span>{{ edits.message.value }}</span>
      </VBanner>
      <label v-if="edits.readyRows.value.length" for="row-edit-reason" class="text-sm"
        >Reason</label
      >
      <VInput
        v-if="edits.readyRows.value.length"
        id="row-edit-reason"
        v-model="reason"
        maxlength="2000"
        placeholder="Why are you changing these rows?"
        aria-required="true"
        :disabled="edits.busy.value"
      />
      <section
        v-if="edits.outcomes.value.length"
        :class="edits.drafts.value.length ? 'mt-4 border-t border-line pt-4' : ''"
        aria-label="Submitted requests"
      >
        <VButton
          size="sm"
          variant="bare"
          icon="history"
          :aria-expanded="historyOpen"
          @click="historyOpen = !historyOpen"
        >
          <span
            >Submitted ({{ edits.outcomes.value.length }}){{
              pending ? ' · ' + pending + ' pending' : ''
            }}{{ notApplied ? ' · ' + notApplied + ' not applied' : '' }}</span
          >
        </VButton>
        <div v-if="historyOpen" class="mt-2">
          <VButton
            size="sm"
            :disabled="edits.refreshing.value || edits.busy.value"
            @click="edits.refreshOutcomes"
          >
            <span>Refresh statuses</span>
          </VButton>
          <p v-if="edits.trackingError.value" class="text-sm text-danger" role="alert">
            {{ edits.trackingError.value }}
          </p>
          <article
            v-for="outcome in edits.outcomes.value"
            :key="outcome.id"
            class="border-b border-line py-2"
          >
            <p class="m-0 font-mono text-xs">
              {{ outcome.row.database }} / {{ outcome.row.schema }}.{{ outcome.row.table }} ·
              {{ outcome.row.keyLabel }}
            </p>
            <p class="m-0 flex items-center gap-2 text-sm" role="status">
              <VIcon
                v-if="outcome.state === 'pending'"
                name="clock"
                size="sm"
                class="text-target"
              />
              <VStatusIcon v-else :status="outcomeStatus(outcome.state)" size="sm" />
              {{ outcomeLabel(outcome.state) }} ·
              {{ outcome.id.slice(0, 8) }}
            </p>
            <p v-if="outcome.notice" class="text-xs text-fg-muted">{{ outcome.notice }}</p>
            <p v-if="outcome.error" class="text-xs text-danger">{{ outcome.error }}</p>
            <div class="flex flex-wrap gap-2">
              <VButton size="sm" variant="bare" :to="'/writes?id=' + outcome.id">
                <span>View request</span>
              </VButton>
              <VButton
                size="sm"
                variant="bare"
                :disabled="edits.busy.value"
                @click="navigate(outcome.row)"
              >
                <span>Show row</span>
              </VButton>
              <VButton
                v-if="
                  ['failed', 'rejected', 'withdrawn'].includes(outcome.state) && !outcome.restored
                "
                size="sm"
                :disabled="edits.busy.value"
                @click="restore(outcome)"
              >
                <span>Restore draft</span>
              </VButton>
            </div>
            <VButton
              v-if="outcome.notice"
              size="sm"
              variant="bare"
              icon="copy"
              @click="copy(outcome.row)"
            >
              <span>Copy submitted draft</span>
            </VButton>
          </article>
        </div>
      </section>
    </div>
    <template v-if="edits.drafts.value.length" #status>
      <VButton
        size="sm"
        variant="bare"
        :aria-expanded="optionsOpen"
        aria-label="Review options"
        title="Review options"
        @click="optionsOpen = !optionsOpen"
      >
        <span aria-hidden="true">⋯</span>
      </VButton>
    </template>
    <template v-if="edits.readyRows.value.length || retryable" #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <VButton
          v-if="
            !edits.busy.value &&
            edits.drafts.value.some((row) => row.error && row.latest === undefined)
          "
          @click="edits.review"
        >
          <span>Retry check</span>
        </VButton>
        <VButton
          v-if="edits.readyRows.value.length"
          variant="primary"
          :icon="approval ? 'send' : 'check'"
          :disabled="!canSubmit"
          @click="submit"
        >
          <span>{{ submitLabel }}</span>
        </VButton>
      </div>
    </template>
  </VDialog>
</template>
