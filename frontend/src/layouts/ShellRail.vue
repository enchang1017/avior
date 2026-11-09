<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { isGroup, type NavEntry } from './navigation'
import { useRail } from './railState'
import { VIcon, VLogo } from '@/components'

/**
 * The instrument rail: every section as an icon, labelled while there is room.
 *
 * Where the operator is carries an amber edge with a glow and the row itself in
 * the target colour — the one place on the rail that is lit. Folded, the rail
 * is icons only with their names as tooltips, and choosing a group unfolds it,
 * because a group's pages need their names to be told apart.
 */
const props = defineProps<{ entries: NavEntry[] }>()

const route = useRoute()
const rail = useRail()

const opened = ref<Set<string>>(new Set())

function containsCurrent(entry: NavEntry): boolean {
  return isGroup(entry) && entry.children.some((child) => child.route === route.name)
}

function isOpen(entry: NavEntry): boolean {
  return !rail.collapsed.value && (opened.value.has(entry.label) || containsCurrent(entry))
}

function toggle(label: string): void {
  if (rail.collapsed.value) {
    rail.expand()
    opened.value = new Set([...opened.value, label])
    return
  }
  const next = new Set(opened.value)
  if (next.has(label)) next.delete(label)
  else next.add(label)
  opened.value = next
}

/** A folded rail names each icon on hover; an open one already says it. */
function tip(name: string): Record<string, string> {
  return rail.collapsed.value ? { title: name } : {}
}

/*
 * The amber edge beside the current row is one bar that slides to it, moved and
 * scaled by transform only. It is drawn at a row's height, so scaling it to a
 * row barely stretches it: a scale of twenty or so once stretched its shadow
 * with it, and the bar overshot the row by more than its own glow. The row is
 * the link the page is on, not its group, when the group is open to show it.
 */
const list = ref<HTMLUListElement | null>(null)
const edgeBar = ref<HTMLLIElement | null>(null)
const edge = ref({ y: 0, scale: 0 })
const sliding = ref(false)

function currentRow(): HTMLElement | null {
  return (
    list.value?.querySelector<HTMLElement>('.rail-item[aria-current="page"]') ??
    list.value?.querySelector<HTMLElement>('.rail-item.current') ??
    null
  )
}

function placeEdge(): void {
  const current = currentRow()
  const drawn = edgeBar.value?.offsetHeight ?? 0
  if (current === null || list.value === null || drawn === 0) {
    edge.value = { y: 0, scale: 0 }
    return
  }
  const box = current.getBoundingClientRect()
  edge.value = { y: box.top - list.value.getBoundingClientRect().top, scale: box.height / drawn }
}

watch([() => route.name, opened, () => rail.collapsed.value], async () => {
  await nextTick()
  placeEdge()
})
let resized: ResizeObserver | null = null
onMounted(async () => {
  await nextTick()
  placeEdge()
  requestAnimationFrame(() => (sliding.value = true))
  if (typeof ResizeObserver === 'function' && list.value !== null) {
    resized = new ResizeObserver(placeEdge)
    resized.observe(list.value)
  }
  void document.fonts?.ready.then(placeEdge)
})
onBeforeUnmount(() => resized?.disconnect())

const label = computed(() => (rail.collapsed.value ? 'sr-only' : 'truncate'))

/*
 * Folded, each icon is named beside it on hover and on keyboard focus (AC-1).
 * The name is the link's own accessible name already; this is its visible
 * twin, so it is hidden from assistive technology.
 */
const tooltip =
  'tip pointer-events-none invisible absolute top-1/2 left-full z-40 ml-3 -translate-y-1/2 rounded-control border border-line-strong bg-surface-raised px-2 py-1 text-xs whitespace-nowrap text-fg shadow-md group-hover:visible group-focus-visible:visible'

function inset(child: boolean): string {
  if (child) return 'h-8 pr-2 pl-9.5'
  return rail.collapsed.value ? 'h-9 justify-center' : 'h-9 px-2.5'
}

function row(current: boolean, child = false): string {
  return [
    'rail-item ease-colours group relative flex w-full cursor-pointer items-center gap-2.5 rounded-control border-0 text-left font-[inherit] text-sm no-underline',
    inset(child),
    current
      ? 'current bg-rail-hover font-medium text-target'
      : 'bg-transparent text-rail-fg hover:bg-rail-hover hover:text-rail-bright',
  ].join(' ')
}
</script>

<template>
  <aside
    class="rail flex flex-none flex-col border-r border-line bg-rail py-3"
    :class="rail.collapsed.value ? 'w-rail-collapsed items-center px-2' : 'w-rail px-2'"
  >
    <RouterLink
      :to="{ name: 'query-console' }"
      class="mb-4 flex h-9 items-center no-underline"
      :class="rail.collapsed.value ? 'justify-center' : 'px-2.5'"
    >
      <VLogo :wordmark="!rail.collapsed.value" size="sm" />
    </RouterLink>

    <nav
      aria-label="Sections"
      class="-mx-2 min-h-0 flex-1 self-stretch px-2"
      :class="rail.collapsed.value ? 'overflow-visible' : 'overflow-y-auto'"
    >
      <ul ref="list" class="relative m-0 flex list-none flex-col gap-0.5 p-0">
        <li
          ref="edgeBar"
          class="edge pointer-events-none absolute top-0 -left-2 h-9 w-0.75 origin-top rounded-pill bg-target shadow-glow-edge"
          :class="[
            sliding ? 'transition-transform duration-200 ease-out' : '',
            edge.scale === 0 ? 'opacity-0' : '',
          ]"
          :style="{ transform: `translateY(${edge.y}px) scaleY(${edge.scale})` }"
          aria-hidden="true"
          role="presentation"
        />
        <li v-for="entry in props.entries" :key="entry.label">
          <template v-if="isGroup(entry)">
            <button
              type="button"
              :class="row(containsCurrent(entry))"
              :aria-expanded="isOpen(entry)"
              v-bind="tip(entry.label)"
              @click="toggle(entry.label)"
            >
              <VIcon :name="entry.icon" size="lg" />
              <span :class="label">{{ entry.label }}</span>
              <span v-if="rail.collapsed.value" :class="tooltip" aria-hidden="true">{{
                entry.label
              }}</span>
              <VIcon
                v-if="!rail.collapsed.value"
                :name="isOpen(entry) ? 'chevron-down' : 'chevron-right'"
                size="sm"
                class="ml-auto text-rail-muted"
              />
            </button>
            <ul v-if="isOpen(entry)" class="m-0 flex list-none flex-col gap-0.5 p-0 pb-1">
              <li v-for="child in entry.children" :key="child.label">
                <RouterLink
                  v-if="child.route"
                  :to="{ name: child.route }"
                  :class="row(route.name === child.route, true)"
                >
                  <span class="truncate">{{ child.label }}</span>
                </RouterLink>
              </li>
            </ul>
          </template>
          <RouterLink
            v-else-if="entry.route"
            :to="{ name: entry.route }"
            :class="row(route.name === entry.route)"
            v-bind="tip(entry.label)"
          >
            <VIcon :name="entry.icon" size="lg" />
            <span :class="label">{{ entry.label }}</span>
            <span v-if="rail.collapsed.value" :class="tooltip" aria-hidden="true">{{
              entry.label
            }}</span>
          </RouterLink>
        </li>
      </ul>
    </nav>

    <div
      class="mt-2 flex w-full items-center gap-1 border-t border-line pt-3"
      :class="rail.collapsed.value ? 'flex-col' : ''"
    >
      <button
        type="button"
        class="fold ml-auto grid size-8 cursor-pointer place-items-center rounded-control border-0 bg-transparent text-rail-fg hover:bg-rail-hover hover:text-rail-bright"
        :class="rail.collapsed.value ? 'ml-0' : ''"
        :aria-label="rail.collapsed.value ? 'Unfold the rail' : 'Fold the rail'"
        :title="rail.collapsed.value ? 'Unfold the rail' : 'Fold the rail'"
        @click="rail.toggle()"
      >
        <VIcon :name="rail.collapsed.value ? 'panel-open' : 'panel-close'" />
      </button>
    </div>
  </aside>
</template>
