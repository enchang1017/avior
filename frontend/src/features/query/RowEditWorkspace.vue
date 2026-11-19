<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { VBanner, VButton, VDialog, VInput, VSelect, VSql, VTable } from '@/components'
import { useSessionStore } from '@/stores/session'
import { useWorkspaceRowEdits, type RowDraft, type WriteOutcome } from './useRowEdits'

// A developer's change ledger: row keys and differences lead, existing neutral
// surfaces and mono type preserve precision, and four-pixel spacing stays dense.
const { bridge, edits } = useWorkspaceRowEdits()
const session = useSessionStore()
const reason = ref('')
const openSQL = ref(new Set<string>())
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
function display(value: string | null | undefined): string {
  return value === null ? 'NULL' : value === undefined ? '—' : JSON.stringify(value)
}
async function open(): Promise<void> {
  if (bridge.flush && !bridge.flush()) return
  bridge.open.value = true
  if (edits.drafts.value.length) await edits.review()
  await edits.refreshOutcomes()
}
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
  <VButton size="sm" icon="list-checks" @click="open">
    <span
      >Changes ({{ edits.count.value }}){{
        pending
          ? ' · ' + pending + ' awaiting approval'
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
    title="Review row changes"
    @close="bridge.open.value = false"
  >
    <div class="max-h-[calc(var(--spacing-dialog-body-max)-var(--spacing)*32)] overflow-auto">
      <p class="mt-0 text-sm">
        Each row is a separate guarded write.
        {{ approval ? 'Changes wait for approval.' : 'Changes save immediately after submission.' }}
        A stale row fails without overwriting newer data.
      </p>
      <p class="text-xs text-fg-muted">
        {{ edits.readyRows.value.length }} ready ·
        {{ edits.drafts.value.filter((row) => row.error).length }} need attention. Only ready rows
        will be submitted. Drafts stay in memory; submitted requests remain available under Writes.
      </p>
      <p v-if="!edits.drafts.value.length" class="text-sm">No pending edits.</p>
      <section v-for="group in groups" :key="group.label" class="mb-4 border-b border-line pb-4">
        <h3 class="font-mono text-sm font-normal">{{ group.label }}</h3>
        <section v-for="row in group.rows" :key="row.id" class="mb-4">
          <p class="font-mono text-xs text-fg-muted">Key: {{ row.keyLabel }}</p>
          <div class="overflow-auto">
            <VTable class="table-fixed">
              <template #caption>Pending values</template>
              <template #head>
                <th scope="col" class="w-20">Column</th>
                <th scope="col">Before</th>
                <th v-if="row.latest" scope="col" class="whitespace-normal">Current database</th>
                <th scope="col">After</th>
                <th v-if="row.latest" scope="col" class="w-40">Resolution</th>
              </template>
              <tr
                v-for="column in row.latest ? Object.keys(row.original) : Object.keys(row.changes)"
                :key="column"
              >
                <th scope="row" class="font-mono text-sm font-normal wrap-anywhere">
                  {{ column }}
                </th>
                <td>
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
                    >{{
                      display(column in row.changes ? row.changes[column] : row.original[column])
                    }}</pre
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
          <div class="flex flex-wrap gap-2 py-2">
            <VButton size="sm" variant="bare" :disabled="edits.busy.value" @click="navigate(row)">
              <span>Return to row</span>
            </VButton>
            <VButton
              size="sm"
              variant="bare"
              :disabled="edits.busy.value"
              @click="edits.discardRow(row.id)"
            >
              <span>Revert this row</span>
            </VButton>
            <VButton size="sm" variant="bare" @click="copy(row)"><span>Copy draft</span></VButton>
            <VButton v-if="row.latest" size="sm" :disabled="edits.busy.value" @click="recheck(row)">
              <span>Use resolutions and recheck</span>
            </VButton>
          </div>
          <p v-if="row.error" class="text-sm text-danger" role="alert">{{ row.error }}</p>
          <div v-if="row.statement">
            <VButton
              size="sm"
              variant="bare"
              :aria-expanded="openSQL.has(row.id)"
              @click="toggleSQL(row.id)"
            >
              <span>Generated SQL · requires exactly one affected row</span>
            </VButton>
            <VSql v-if="bridge.open.value && openSQL.has(row.id)" :code="row.statement" />
          </div>
        </section>
      </section>
      <VBanner v-if="edits.message.value" tone="info">
        <span>{{ edits.message.value }}</span>
      </VBanner>
      <label v-if="edits.drafts.value.length" for="row-edit-reason" class="text-sm">Reason</label>
      <VInput
        v-if="edits.drafts.value.length"
        id="row-edit-reason"
        v-model="reason"
        maxlength="2000"
        :disabled="edits.busy.value"
      />
      <section
        v-if="edits.outcomes.value.length"
        class="mt-4 border-t border-line pt-4"
        aria-label="Submitted requests"
      >
        <h3 class="text-sm">Submitted requests</h3>
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
          <p class="m-0 text-sm" role="status">
            {{ outcome.state === 'pending' ? 'Awaiting approval' : outcome.state }} ·
            {{ outcome.id.slice(0, 8) }}
          </p>
          <p v-if="outcome.notice" class="text-xs text-fg-muted">{{ outcome.notice }}</p>
          <p v-if="outcome.error" class="text-xs text-danger">{{ outcome.error }}</p>
          <div class="flex flex-wrap gap-2">
            <VButton size="sm" variant="bare" :to="'/writes?id=' + outcome.id">
              <span>View request</span>
            </VButton>
            <VButton size="sm" variant="bare" @click="navigate(outcome.row)">
              <span>{{ outcome.state === 'applied' ? 'Refresh row' : 'Return to row' }}</span>
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
            <VButton size="sm" variant="bare" @click="copy(outcome.row)">
              <span>Copy submitted draft</span>
            </VButton>
          </div>
        </article>
      </section>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <VButton :disabled="edits.busy.value" @click="bridge.open.value = false">
          <span>Keep editing</span>
        </VButton>
        <VButton :disabled="edits.busy.value || !edits.drafts.value.length" @click="edits.review">
          <span>Check again</span>
        </VButton>
        <VButton
          v-if="edits.drafts.value.length"
          variant="primary"
          :disabled="!canSubmit"
          @click="submit"
        >
          <span>{{ approval ? 'Submit for approval' : 'Save changes' }}</span>
        </VButton>
      </div>
    </template>
  </VDialog>
</template>
