<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { VBadge, VButton, VInput, VLoader, VSql, VTrajectory } from '@/components'
import { writeTrajectory } from '@/components/Data/VTrajectory/trajectory'
import { useSessionStore } from '@/stores/session'
import { fullStamp } from '@/shared/timestamp'
import { since } from './since'
import { writeStepDetails } from './steps'
import { isLarge, isSettled, type WriteRequestDetail } from './useWriteRequests'

/** The parts of the requests page the detail reads and acts through. */
export type DetailPage = {
  detail: { value: WriteRequestDetail | null }
  deciding: { value: string }
  opening: { value: string }
  conflict: { value: { code: string; message: string } | null }
  decide: (id: string, decision: 'approve' | 'reject', note: string) => Promise<void>
  withdraw: (id: string) => Promise<void>
}

/**
 * The right half of the requests page: one request, whole, with the decision
 * under it.
 *
 * Part of the page rather than over it, so the list stays where it was while
 * the reader works down it. The body scrolls and the decision does not: an
 * approver at the bottom of a long statement and one at the top are one
 * keystroke from the same buttons.
 */
const props = defineProps<{ page: DetailPage; canDecide: boolean; canWithdraw: boolean }>()
const emit = defineEmits<{ back: [] }>()

const session = useSessionStore()
const note = ref('')

const toneText = {
  target: 'text-target',
  halt: 'text-danger',
  go: 'text-ok',
  muted: 'text-fg-muted',
}

const detail = computed(() => props.page.detail.value)
const request = computed(() => detail.value?.request ?? null)
const trajectory = computed(() =>
  request.value === null ? null : writeTrajectory(request.value.state),
)
const steps = computed(() => (detail.value === null ? [] : writeStepDetails(detail.value)))
const opening = computed(() => props.page.opening.value)
const busy = computed(() => props.page.deciding.value !== '' || opening.value !== '')
const conflict = computed(() => props.page.conflict.value?.message ?? null)
const ownNotice = computed(() => {
  const current = request.value
  if (current === null || isSettled(current.state)) return null
  if (current.requestedBy !== session.caller?.username) return null
  return session.caller.selfApproval
    ? 'You asked for this change and this deployment lets you decide it yourself. Production does not: there a second person reads it.'
    : 'You asked for this change, so somebody else has to decide it. That second person is the whole of the check.'
})

const rows = new Intl.NumberFormat('en')

watch(
  () => request.value?.id,
  () => {
    note.value = ''
  },
)

async function settle(decision: 'approve' | 'reject'): Promise<void> {
  if (request.value === null) return
  await props.page.decide(request.value.id, decision, note.value)
  note.value = ''
}

async function withdraw(): Promise<void> {
  if (request.value !== null) await props.page.withdraw(request.value.id)
}

async function copy(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    // A refused clipboard is not worth an error banner: the operator can still
    // select the text. Silence here is the whole handling, deliberately.
  }
}
</script>

<template>
  <article class="flex min-h-0 flex-1 flex-col" data-testid="request-detail">
    <div class="min-h-0 flex-1 overflow-auto px-5 pt-3 pb-6" data-scroll>
      <VButton
        class="mb-2 lg:hidden"
        variant="bare"
        size="sm"
        icon="arrow-left"
        @click="emit('back')"
      >
        All requests
      </VButton>

      <div v-if="opening" class="flex justify-center py-10">
        <VLoader size="section" label="Opening the request" />
      </div>

      <template v-else-if="request && trajectory">
        <header class="flex flex-col gap-1 border-b border-line pb-3">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h2
              tabindex="-1"
              class="m-0 font-mono text-lg font-semibold outline-none"
              :title="request.id"
            >
              #{{ request.id.slice(0, 8) }}
            </h2>
            <VBadge class="uppercase">{{ request.kind }}</VBadge>
            <span
              class="font-mono text-3xs tracking-hud uppercase"
              :class="toneText[trajectory.tone]"
            >
              {{ trajectory.caption }}
            </span>
          </div>
          <p class="m-0 text-sm text-fg-soft">
            {{ request.requestedBy }} ·
            <span :title="fullStamp(request.requestedAt)">{{ since(request.requestedAt) }}</span> ·
            <span class="font-mono">{{ request.database }}</span>
          </p>
        </header>

        <dl class="m-0 mt-4 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <dt class="text-fg-muted">Why</dt>
          <dd class="reason m-0">{{ request.reason }}</dd>
          <dt class="text-fg-muted">Target</dt>
          <dd class="m-0 font-mono">{{ request.database }} › {{ request.targetTable }}</dd>
          <dt class="text-fg-muted">Rows</dt>
          <dd class="m-0">
            <span :class="{ 'font-bold text-warn': isLarge(request.estimatedRows) }">
              {{ rows.format(request.estimatedRows) }} estimated
            </span>
            <span v-if="request.affectedRows !== undefined" class="text-fg-soft">
              → {{ rows.format(request.affectedRows) }} changed
            </span>
          </dd>
        </dl>

        <h3 class="m-0 mt-5 mb-2 eyebrow">Statement</h3>
        <VSql :code="request.statement" size="full" :wrap="false" />

        <template v-if="request.reverseStatement">
          <div class="mt-5 mb-2 flex items-center justify-between gap-4">
            <h3 class="m-0 eyebrow">To undo it</h3>
            <VButton size="sm" variant="ghost" icon="copy" @click="copy(request.reverseStatement)">
              Copy
            </VButton>
          </div>
          <p class="m-0 mb-2 text-sm text-fg-soft">
            <template v-if="request.reversible">
              {{ rows.format(request.captureRows ?? 0) }} row(s) kept in
              <code>avior_backup.captured_row</code> in <strong>{{ request.database }}</strong
              >, keyed by this request.
            </template>
            <span v-else class="line-through">
              The backup has been removed, so this will not run.
            </span>
          </p>
          <VSql :code="request.reverseStatement" size="full" :wrap="false" />
        </template>

        <h3 class="m-0 mt-5 mb-3 eyebrow">Approval steps</h3>
        <VTrajectory :trajectory="trajectory" layout="steps" :details="steps" />

        <p v-if="request.error" class="run-error m-0 mt-4 text-sm text-danger">
          {{ request.error }}
        </p>
      </template>
    </div>

    <footer
      v-if="request && !opening"
      class="decision flex flex-none flex-col gap-2 border-t border-line bg-surface px-5 py-3"
    >
      <p v-if="conflict" class="detail-message m-0 text-sm text-danger">{{ conflict }}</p>
      <p v-else-if="ownNotice" class="own-notice m-0 text-sm text-fg-soft">{{ ownNotice }}</p>
      <div v-if="canDecide" class="flex items-center gap-2">
        <span class="mr-auto flex max-w-112 min-w-0 flex-1">
          <VInput v-model="note" size="sm" placeholder="Note (optional)" aria-label="Note" />
        </span>
        <VButton :disabled="busy" danger @click="settle('reject')">Reject</VButton>
        <VButton variant="primary" icon="check" :disabled="busy" @click="settle('approve')">
          Approve and run
        </VButton>
      </div>
      <div v-else-if="canWithdraw" class="flex justify-end">
        <VButton :disabled="busy" @click="withdraw">Withdraw</VButton>
      </div>
      <p v-else-if="!conflict && !ownNotice" class="m-0 text-sm text-fg-muted">
        {{
          isSettled(request.state)
            ? 'Decided. Nothing is left to do here.'
            : 'Waiting for somebody holding write.approve.'
        }}
      </p>
    </footer>
  </article>
</template>
