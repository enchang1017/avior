<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { useWriteCaptures, describeAge, type WriteCapture } from './useWriteCaptures'
import {
  VBanner,
  VButton,
  VDialog,
  VEmpty,
  VField,
  VInput,
  VPageHeader,
  VSkeleton,
  VSql,
  VTable,
} from '@/components'
import { describeFailure } from '@/shared/api'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

/**
 * FEAT-009 AC-16 to AC-19. What the write path has kept, and the one place a
 * DBA removes some of it.
 *
 * Nothing is removed on a schedule, and the page says so rather than leaving
 * somebody to infer it: a capture is the only copy of rows somebody changed,
 * and seven days is when it is marked, not when anything happens to it.
 */
const page = useWriteCaptures()
const session = useSessionStore()

// Undoing a governed write is the same class of decision as letting one in, so
// it is the role that exists for that judgement rather than the one that owns
// this page. A DBA who is not an approver sees what was kept and cannot re-run
// production changes from here.
const mayUndo = computed(() => session.caller?.permissions.includes('write.approve') === true)

/**
 * Whether this backup's change can still be put back.
 *
 * A rollback does not remove the capture it read — the rows stay, and a DBA may
 * still want to prune them — so a request that was already undone goes on
 * appearing here. Offering Undo on one is a control certain to be refused, and
 * that is what it did: the button answered "this change has already been put
 * back" from behind the dialog it was pressed in.
 */
function undoable(capture: WriteCapture): boolean {
  return (
    mayUndo.value &&
    capture.request.rolledBackBy === undefined &&
    // An undo is not undone from here. Putting its change back is a new change
    // to production, so it is asked for again and approved like any other.
    capture.request.rollbackOf === undefined
  )
}

/** A change that was undone, which somebody may want to ask for again. */
function askAgain(capture: WriteCapture): boolean {
  return capture.request.rolledBackBy !== undefined
}

const bannerText = computed(() => {
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

const asking = ref<WriteCapture | null>(null)
const undoing = ref<WriteCapture | null>(null)
const undoReason = ref('')

// The dialog's own line, so an answer about the backup somebody is looking at
// does not land behind the dialog they are looking at it in.
const dialogMessage = computed(() => {
  const conflict = page.conflict.value
  if (conflict !== null) return conflict.message
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

/**
 * Which rows have their reverse statement open.
 *
 * On this page rather than only in a dialog, because deciding whether a backup
 * can go means reading the statement it would stop working — and a DBA doing
 * that down a list should not have to open eight modals to see eight
 * statements.
 */
const captureHeadings = [
  'Request',
  'Age',
  'Database',
  'Table',
  'Why',
  'Rows kept',
  'Asked by',
  'Ran',
  '',
]
const captureColumns = [
  'w-21/25',
  'w-1/3',
  'w-21/25',
  'w-21/25',
  'w-full',
  'w-1/3',
  'w-1/2',
  'w-1/2',
  'w-1/3',
]

const opened = ref(new Set<string>())

function toggle(id: string): void {
  // A new Set rather than mutating: a Set changed in place is the same object,
  // and Vue's reactivity would not see it.
  const next = new Set(opened.value)
  if (!next.delete(id)) next.add(id)
  opened.value = next
}

async function copy(capture: WriteCapture): Promise<void> {
  try {
    await navigator.clipboard.writeText(capture.request.reverseStatement ?? '')
  } catch {
    // A refused clipboard is not worth an error banner: the operator can still
    // select the text. Silence here is the whole handling, deliberately.
  }
}

const rows = new Intl.NumberFormat()

/**
 * The first eight characters of a request's id.
 *
 * A page of rows that all read "public.bet, 1 row, developer" is a page nobody
 * can tell apart, and the full uuid is thirty-six characters of noise in a
 * column. Eight is enough to match a row against the one named in a reverse
 * statement or an error, and the whole of it is on the row's title.
 */
function shortId(id: string): string {
  return id.slice(0, 8)
}

function at(value: string | undefined): string {
  return value === undefined || value === '' ? '—' : stamp(value)
}

async function undo(): Promise<void> {
  const target = undoing.value
  if (target === null) return
  if (await page.undo(target.request.id, undoReason.value)) {
    undoing.value = null
    undoReason.value = ''
  }
}

async function remove(): Promise<void> {
  const target = asking.value
  if (target === null) return
  if (await page.remove(target.request.id)) asking.value = null
}

/*
 * Its own line and its own weight, so it is a mark on the row rather than more
 * of the sentence the requester wrote.
 */
const undoneMark =
  'mt-1 inline-block rounded-control border border-line-strong px-1.5 py-0.25 text-xs text-fg-soft'

const dialogActions = 'mt-4 flex justify-end gap-2 border-t border-line pt-4'

onMounted(() => void page.load())
</script>

<template>
  <!--
    The header spans the pane and the content under it scrolls.

    Page-wide padding inset the header's own background from the edges, so the
    banner across the top stopped at a margin and the pane showed through around
    it. Every other page in the app puts the padding on what is below instead.
  -->
  <div class="captures flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-ground">
    <VPageHeader
      icon="backup"
      title="Backups the write path has kept"
      subtitle="Kept until somebody removes them. Past seven days is marked."
      :busy="page.busy.value"
    />

    <div class="min-h-0 flex-1 overflow-auto px-5 pt-4 pb-8">
      <div class="mb-4 flex items-center gap-4">
        <label class="flex items-center gap-[--spacing(1.6)]">
          <input v-model="page.staleOnly.value" type="checkbox" />
          Older than seven days only
          <span class="text-fg-soft">({{ page.staleCount.value }})</span>
        </label>
        <VButton size="sm" variant="ghost" :disabled="page.busy.value" @click="page.load()">
          Refresh
        </VButton>
      </div>

      <VBanner v-if="bannerText">{{ bannerText }}</VBanner>
      <VBanner v-if="page.conflict.value">{{ page.conflict.value.message }}</VBanner>

      <div class="overflow-x-auto">
        <VTable v-if="page.shown.value.length > 0" variant="requests">
          <template #caption>Backups, newest first</template>
          <template #head>
            <th scope="col">Request</th>
            <th scope="col">Age</th>
            <th scope="col">Database</th>
            <th scope="col">Table</th>
            <th scope="col">Why</th>
            <th scope="col">Rows kept</th>
            <th scope="col">Asked by</th>
            <th scope="col">Ran</th>
            <th scope="col"><span class="sr-only">Actions</span></th>
          </template>
          <template v-for="capture in page.shown.value" :key="capture.request.id">
            <tr>
              <td class="font-mono text-fg-soft" :title="capture.request.id">
                {{ shortId(capture.request.id) }}
              </td>
              <td :class="{ 'font-bold': capture.stale }">
                {{ describeAge(capture.ageHours) }}
                <span v-if="capture.stale" class="ml-[--spacing(1.6)] font-normal text-fg-soft">
                  past a week
                </span>
              </td>
              <td>{{ capture.request.database }}</td>
              <td class="font-mono">{{ capture.request.targetTable }}</td>
              <!-- Why somebody asked is half of what a reader decides on, so it is
                   a column here as well as on the request list. The one cell
                   allowed to wrap. -->
              <td class="max-w-2xs whitespace-normal">
                <span class="block">{{ capture.request.reason }}</span>
                <!-- Already undone: the rows are still kept, and there is nothing
                     left to put back. Saying so is why Undo is not offered — and
                     it is a mark rather than a tail on the reason, which read as
                     one sentence. -->
                <span v-if="capture.request.rolledBackBy" :class="undoneMark">
                  already put back
                </span>
                <span v-else-if="capture.request.rollbackOf" :class="undoneMark">an undo</span>
              </td>
              <td>{{ rows.format(capture.request.captureRows ?? 0) }}</td>
              <td>{{ capture.request.requestedBy }}</td>
              <td :title="fullStamp(capture.request.ranAt ?? '')">
                {{ at(capture.request.ranAt) }}
              </td>
              <!-- A cell, with a flex row inside it. `display: flex` on a `<td>`
                   takes it out of the table's column algorithm, so the last
                   column stopped sharing a width with the rows above it and the
                   buttons walked off the right edge. The cell stays a cell. -->
              <td class="actions-cell w-1/100 whitespace-nowrap">
                <div class="flex justify-end gap-1.5">
                  <VButton
                    size="sm"
                    variant="ghost"
                    :icon="opened.has(capture.request.id) ? 'chevron-down' : 'chevron-right'"
                    @click="toggle(capture.request.id)"
                  >
                    Undo SQL
                  </VButton>
                  <VButton
                    v-if="askAgain(capture)"
                    size="sm"
                    variant="ghost"
                    icon="replay"
                    title="Open the composer with this statement, to ask for the change again"
                    :to="{ name: 'write-compose', query: { from: capture.request.id } }"
                  >
                    Ask again
                  </VButton>
                  <VButton
                    v-if="undoable(capture)"
                    size="sm"
                    variant="primary"
                    icon="undo"
                    :disabled="page.removing.value !== ''"
                    @click="undoing = capture"
                  >
                    Undo
                  </VButton>
                  <VButton
                    size="sm"
                    variant="ghost"
                    :disabled="page.removing.value !== ''"
                    @click="asking = capture"
                  >
                    Remove
                  </VButton>
                </div>
              </td>
            </tr>

            <tr v-if="opened.has(capture.request.id)" class="undo" data-detail>
              <td :colspan="9">
                <div class="mb-2 flex items-center justify-between gap-4 text-fg-soft">
                  <span>
                    Kept in
                    <code>avior_backup.captured_row</code>
                    in <strong>{{ capture.request.database }}</strong
                    >, keyed by this request.
                  </span>
                  <VButton size="sm" variant="ghost" icon="copy" @click="copy(capture)">
                    Copy
                  </VButton>
                </div>
                <VSql :code="capture.request.reverseStatement ?? ''" size="lg" />
              </td>
            </tr>
          </template>
        </VTable>

        <VSkeleton
          v-else-if="page.busy.value"
          variant="table"
          table="requests"
          :headings="captureHeadings"
          :columns="captureColumns"
          :row-lines="2"
          label="Reading backups"
        />
        <VEmpty v-else>
          {{
            page.staleOnly.value
              ? 'No backup is older than seven days.'
              : 'The write path has kept nothing yet.'
          }}
        </VEmpty>
      </div>
    </div>

    <VDialog :open="undoing !== null" title="Put these rows back" @close="undoing = null">
      <template v-if="undoing">
        <p class="m-0 mb-3 max-w-lg">
          This runs the statement below against
          <strong>{{ undoing.request.database }}</strong
          >, putting the
          <strong>{{ rows.format(undoing.request.captureRows ?? 0) }}</strong>
          rows back as they were.
        </p>
        <p class="m-0 mb-3 max-w-lg text-fg-soft">
          It becomes a write request of its own, with its own backup — so this can be undone in
          turn. Avior wrote the statement from what was kept; nothing you type reaches it.
        </p>

        <VSql :code="undoing.request.reverseStatement ?? ''" size="lg" />

        <!-- In the dialog it was pressed in, rather than in a banner behind it. -->
        <p
          v-if="dialogMessage"
          class="m-0 mt-4 max-w-2xl rounded-control border border-danger bg-surface-sunken px-3 py-2.5"
        >
          {{ dialogMessage }}
        </p>

        <VField label="Why" for="undo-reason" wide>
          <VInput id="undo-reason" v-model="undoReason" type="text" autocomplete="off" />
        </VField>

        <div :class="dialogActions">
          <VButton variant="ghost" @click="undoing = null">Cancel</VButton>
          <VButton
            variant="primary"
            icon="undo"
            :disabled="undoReason.trim() === '' || page.removing.value !== ''"
            @click="undo()"
          >
            Put them back
          </VButton>
        </div>
      </template>
    </VDialog>

    <VDialog :open="asking !== null" title="Remove this backup" @close="asking = null">
      <template v-if="asking">
        <p class="m-0 mb-3 max-w-lg">
          This deletes the
          <strong>{{ rows.format(asking.request.captureRows ?? 0) }}</strong>
          rows kept for a write against
          <strong class="font-mono">{{ asking.request.targetTable }}</strong>
          in
          <strong>{{ asking.request.database }}</strong
          >. After this the write can no longer be undone.
        </p>
        <p class="m-0 mb-3 max-w-lg text-fg-soft">
          The request itself stays, and will say when the backup went and who removed it.
        </p>

        <h3 class="mx-0 mt-4 mb-2 text-md font-semibold text-fg-soft">What would no longer run</h3>
        <VSql :code="asking.request.reverseStatement ?? ''" size="md" />

        <div :class="dialogActions">
          <VButton variant="ghost" @click="asking = null">Keep it</VButton>
          <!-- Danger, because this is the only copy of rows somebody changed and
               nothing puts it back. -->
          <VButton
            variant="primary"
            danger
            icon="trash"
            :disabled="page.removing.value !== ''"
            @click="remove()"
          >
            Remove
          </VButton>
        </div>
      </template>
    </VDialog>
  </div>
</template>
