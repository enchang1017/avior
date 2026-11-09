<script setup lang="ts">
import { VStatusIcon, VTrajectory, type Status } from '@/components'
import { writeTrajectory } from '@/components/Data/VTrajectory/trajectory'
import { fullStamp, stamp } from '@/shared/timestamp'
import { isLarge, type WriteRequest, type WriteRequestState } from './useWriteRequests'

/**
 * The left half of the requests page: one line of facts per request, enough
 * to tell them apart and to pick the next one, and nothing an approver decides
 * on. That is the detail's job, beside it.
 */
const props = defineProps<{ requests: readonly WriteRequest[]; selected: string }>()
const emit = defineEmits<{
  select: [id: string]
  /** Enter on a request: read it, with the keyboard now in the detail. */
  enter: [id: string]
  /** An arrow key on a focused request: the page moves the selection. */
  step: [step: 1 | -1]
}>()

const statusOf: Record<WriteRequestState, Status> = {
  pending: 'pending',
  applied: 'ok',
  rejected: 'refused',
  withdrawn: 'idle',
  failed: 'warn',
}

const rows = new Intl.NumberFormat('en')

function onKeydown(event: KeyboardEvent, id: string): void {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    emit('step', event.key === 'ArrowDown' ? 1 : -1)
  } else if (event.key === 'Enter') {
    event.preventDefault()
    emit('enter', id)
  }
}
</script>

<template>
  <ul class="m-0 list-none p-0" aria-label="Write requests">
    <li v-for="request in props.requests" :key="request.id">
      <button
        type="button"
        class="relative flex w-full cursor-pointer flex-col gap-1 border-0 border-b border-line px-4 py-2.5 text-left text-fg hover:bg-surface-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary"
        :class="props.selected === request.id ? 'bg-primary-wash' : 'bg-ground'"
        :data-request="request.id"
        :aria-current="props.selected === request.id"
        :title="request.reason"
        @click="emit('select', request.id)"
        @keydown="onKeydown($event, request.id)"
      >
        <span
          v-if="props.selected === request.id"
          class="absolute inset-y-0 left-0 w-0.75 bg-primary"
          aria-hidden="true"
        />
        <span class="flex w-full items-center gap-2">
          <VStatusIcon :status="statusOf[request.state]" size="sm" />
          <span class="font-mono text-xs text-fg-soft">#{{ request.id.slice(0, 8) }}</span>
          <span
            class="text-xs capitalize"
            :class="request.state === 'failed' ? 'text-danger' : 'text-fg-soft'"
            :data-state="request.state"
            >{{ request.state }}</span
          >
          <span
            class="ml-auto text-xs text-fg-muted"
            :title="fullStamp(request.requestedAt)"
            data-moment
            >{{ stamp(request.requestedAt) }}</span
          >
        </span>
        <span class="w-full truncate font-mono text-sm">
          {{ request.database }} › {{ request.targetTable }}
        </span>
        <span class="reason w-full truncate text-sm text-fg-soft">{{ request.reason }}</span>
        <span class="flex w-full items-center gap-2 text-xs text-fg-muted">
          <span class="min-w-0 truncate">{{ request.requestedBy }}</span>
          <span
            class="whitespace-nowrap"
            :class="{ 'font-bold text-warn': isLarge(request.estimatedRows) }"
            >{{ rows.format(request.estimatedRows) }}
            {{ request.estimatedRows === 1 ? 'row' : 'rows' }}</span
          >
          <VTrajectory :trajectory="writeTrajectory(request.state)" class="ml-auto flex-none" />
        </span>
      </button>
    </li>
  </ul>
</template>
