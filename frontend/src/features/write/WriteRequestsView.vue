<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { requestScopes, stateTabs, visibleRequests } from './requestFilters'
import {
  useWriteRequests,
  isSettled,
  type WriteRequestState,
  type WriteScope,
} from './useWriteRequests'
import {
  VBanner,
  VButton,
  VEmpty,
  VField,
  VFilterBar,
  VPageHeader,
  VScopeSearch,
  VSelect,
  VSkeleton,
  VTabs,
  parseQuery,
} from '@/components'
import WriteRequestDetail from './WriteRequestDetail.vue'
import WriteRequestList from './WriteRequestList.vue'
import { isTyping, neighbour } from './selection'
import { describeFailure } from '@/shared/api'
import { useSessionStore } from '@/stores/session'

const props = defineProps<{ scope: WriteScope }>()

// The scope arrives as a getter for the reason the release queue's does: two
// routes share one component and Vue Router reuses the instance rather than
// remounting, so a scope captured at setup would leave the page reading
// whichever list was opened first.
const page = useWriteRequests(() => props.scope)
const session = useSessionStore()
const route = useRoute()
const router = useRouter()

const listPane = ref<HTMLElement | null>(null)
const detailPane = ref<HTMLElement | null>(null)

const mayDecide = computed(() => session.caller?.permissions.includes('write.approve') === true)

/**
 * Whether this is the reader's own request.
 *
 * A requester cannot decide their own, whatever roles they hold — the second
 * person is the whole of the check. Offering the button anyway and answering
 * `403` after the click is a control that is certain to be refused, which the
 * account page already refuses to draw for the same reason.
 */
const isOwn = computed(() => page.detail.value?.request.requestedBy === session.caller?.username)

/**
 * Whether this deployment lets the author decide.
 *
 * Read from the deployment rather than assumed, because withholding the control
 * where it would have been accepted is its own failure: the mode exists so one
 * operator can rehearse the approval path, and a page that hides the decision
 * makes that path unreachable through the interface meant to rehearse it.
 *
 * Never true in production — Avior refuses to start on that setting — so this
 * does not weaken the check where the check is the point.
 */
const selfApproval = computed(() => session.caller?.selfApproval === true)

const canDecide = computed(
  () =>
    mayDecide.value &&
    (!isOwn.value || selfApproval.value) &&
    page.detail.value !== null &&
    !isSettled(page.detail.value.request.state),
)

const canWithdraw = computed(
  () => isOwn.value && page.detail.value !== null && !isSettled(page.detail.value.request.state),
)
const maySubmit = computed(() => session.caller?.permissions.includes('query.execute') === true)

const bannerText = computed(() => {
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

const heading = computed(() =>
  props.scope === 'queue' ? 'Write requests waiting' : 'Write requests',
)

/*
 * Issue-style: status tabs counted from what was loaded, and a search whose
 * `database:`, `table:` and `by:` words become chips. Both narrow what the page
 * already holds, so switching a tab never waits on the server.
 */
const tab = ref<WriteRequestState | ''>(props.scope === 'queue' ? 'pending' : '')
const search = ref('')
const tabs = computed(() => stateTabs(page.requests.value, props.scope))
const shown = computed(() =>
  visibleRequests(page.requests.value, tab.value, parseQuery(search.value, requestScopes)),
)

const whoseOptions = [
  { value: 'all', label: 'Everybody’s' },
  { value: 'mine', label: 'Only mine' },
]

/*
 * The request being read lives in the address, so a link to one opens it and
 * the browser's back button walks back through what was read.
 */
const selected = computed(() => {
  const id = route.query['open']
  return typeof id === 'string' ? id : ''
})

function select(id: string): void {
  if (id === selected.value) return
  void router.replace({ query: { ...route.query, open: id } })
}

function clear(): void {
  void router.replace({
    query: Object.fromEntries(Object.entries(route.query).filter(([key]) => key !== 'open')),
  })
}

watch(
  selected,
  (id) => {
    if (id === '') page.close()
    else void page.open(id)
  },
  { immediate: true },
)

/** Whether the list and the detail sit side by side, rather than one at a time. */
function sideBySide(): boolean {
  return typeof window.matchMedia !== 'function' || window.matchMedia('(min-width: 64rem)').matches
}

/*
 * Somebody opening the waiting list is there to work through it, so the first
 * request is open before they ask. Only beside the list: on a narrow screen
 * the detail replaces the list, and opening one unasked would hide the queue.
 */
function pickFirst(): void {
  const first = shown.value[0]
  if (selected.value !== '' || tab.value !== 'pending' || first === undefined) return
  if (sideBySide()) select(first.id)
}

async function load(): Promise<void> {
  await page.load()
  pickFirst()
}

async function step(by: 1 | -1): Promise<void> {
  const next = neighbour(
    shown.value.map((request) => request.id),
    selected.value,
    by,
  )
  if (next === null) return
  select(next)
  await nextTick()
  const item = listPane.value?.querySelector<HTMLElement>(`[data-request="${CSS.escape(next)}"]`)
  item?.focus()
  item?.scrollIntoView({ block: 'nearest' })
}

/** The request whose detail takes the keyboard as soon as it is on screen. */
let focusWhenRead = ''

async function enter(id: string): Promise<void> {
  focusWhenRead = id
  select(id)
  await nextTick()
  if (page.detail.value?.request.id === id) settleFocus()
}

function settleFocus(): void {
  if (focusWhenRead === '' || page.detail.value?.request.id !== focusWhenRead) return
  focusWhenRead = ''
  void nextTick(() => detailPane.value?.querySelector<HTMLElement>('h2[tabindex]')?.focus())
}

watch(() => page.detail.value?.request.id, settleFocus)

/*
 * j and k anywhere on the page, the way a mail client reads a queue. Not while
 * somebody is typing — a note, a search — and not under a dialog, which owns
 * the keyboard while it is open.
 */
function onKeydown(event: KeyboardEvent): void {
  if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return
  if (event.key !== 'j' && event.key !== 'k') return
  if (isTyping(event.target) || document.querySelector('dialog[open]') !== null) return
  event.preventDefault()
  void step(event.key === 'j' ? 1 : -1)
}

watch(
  () => props.scope,
  (scope) => {
    page.state.value = ''
    tab.value = scope === 'queue' ? 'pending' : ''
    void load()
  },
)

watch(tab, pickFirst)

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  void load()
})

onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-ground">
    <VPageHeader
      icon="pull-request"
      :title="heading"
      :count="page.requests.value.length"
      :busy="page.busy.value"
    >
      <VButton
        v-if="maySubmit && scope === 'mine'"
        size="sm"
        variant="primary"
        icon="plus"
        :to="{ name: 'write-compose' }"
      >
        Ask for a change
      </VButton>
    </VPageHeader>

    <VFilterBar :busy="page.busy.value" @submit="page.refresh()">
      <VField label="Search" for="write-search" wide>
        <VScopeSearch
          id="write-search"
          v-model="search"
          :keys="requestScopes"
          placeholder="state:, database:, requester: or any word"
        />
      </VField>
      <VField label="Whose" for="write-mine">
        <VSelect
          id="write-mine"
          :model-value="page.mine.value ? 'mine' : 'all'"
          :options="whoseOptions"
          @update:model-value="page.mine.value = $event === 'mine'"
        />
      </VField>
    </VFilterBar>

    <VTabs v-model="tab" :tabs="tabs" label="Request state" class="flex-none px-5" />

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

    <!--
      Master and detail, side by side and both part of the page: the list stays
      where it was while the reader works down it, and the request being read
      has the width its SQL needs. Below the breakpoint they take turns, and
      the detail carries the way back.
    -->
    <div
      class="split grid min-h-0 flex-1 grid-cols-1 grid-rows-[minmax(0,1fr)] border-t border-line lg:grid-cols-[var(--spacing-queue)_minmax(0,1fr)]"
    >
      <section
        ref="listPane"
        class="min-h-0 overflow-auto lg:border-r lg:border-line"
        :class="{ 'max-lg:hidden': selected !== '' }"
        aria-label="Requests"
      >
        <WriteRequestList
          v-if="shown.length > 0"
          :requests="shown"
          :selected="selected"
          @select="select"
          @enter="enter"
          @step="step"
        />
        <VSkeleton
          v-else-if="page.busy.value"
          variant="list"
          :rows="6"
          :row-lines="4"
          label="Reading write requests"
        />
        <VEmpty v-else-if="page.requests.value.length > 0"> Nothing here matches. </VEmpty>
        <VEmpty v-else>
          {{
            scope === 'queue'
              ? 'Nothing is waiting for a decision.'
              : 'Nobody has asked for a row change.'
          }}
        </VEmpty>
      </section>

      <section
        ref="detailPane"
        class="flex min-h-0 min-w-0 flex-col"
        :class="{ 'max-lg:hidden': selected === '' }"
        aria-label="The request being read"
      >
        <WriteRequestDetail
          v-if="selected !== ''"
          :page="page"
          :can-decide="canDecide"
          :can-withdraw="canWithdraw"
          @back="clear"
        />
        <VEmpty v-else>
          Choose a request to read it. <kbd class="font-mono">j</kbd> and
          <kbd class="font-mono">k</kbd> move through the list.
        </VEmpty>
      </section>
    </div>
  </div>
</template>
