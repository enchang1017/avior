<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'
import { arrange, searchEach, type PaletteSlot } from './answers'
import { usePalette, type PaletteAction, type PaletteItem } from '@/shared/palette'

/**
 * ⌘K: find a table, a request, an operator.
 *
 * The platform `<dialog>` for the focus trap and the top layer, placed high on
 * the page the way a palette is, and nothing but a field and a list. What it
 * finds comes from the sources registered in `shared/palette.ts`, each painted
 * the moment it answers; every keystroke aborts the searches before it, and an
 * answer to an older query is dropped rather than painted over a newer one.
 */
const palette = usePalette()

const element = ref<HTMLDialogElement | null>(null)
const field = ref<HTMLInputElement | null>(null)
const query = ref('')
const slots = shallowRef<(PaletteSlot | undefined)[]>([])
const arrival = shallowRef<number[]>([])
const pending = ref(0)
const list = ref<HTMLElement | null>(null)
const marker = ref<{ transform: string; height: string } | null>(null)
const active = ref(0)
/** Which of the active row's destinations is chosen; the first is the row's own. */
const action = ref(0)
let asking: AbortController | null = null
let latest = 0

const connected = computed(() => palette.sources.value.length > 0)
const shown = computed(() => arrange(slots.value, arrival.value))
const found = computed(() => shown.value.flat)
const activeActions = computed(() => found.value[active.value]?.actions ?? [])
const optionId = (index: number, destination?: number) =>
  destination === undefined
    ? `palette-option-${index}`
    : `palette-option-${index}-action-${destination}`
const activeAttr = computed((): Record<string, string> => {
  if (found.value[active.value] === undefined) return {}
  const destination = activeActions.value.length > 0 ? action.value : undefined
  return { 'aria-activedescendant': optionId(active.value, destination) }
})
watch([active, found], () => (action.value = 0))

function stop(): void {
  asking?.abort()
  asking = null
  latest += 1
}

async function follow(open: boolean): Promise<void> {
  const dialog = element.value
  if (dialog === null) return
  if (open && !dialog.open) {
    dialog.showModal()
    await nextTick()
    field.value?.focus()
  }
  if (!open) stop()
  if (!open && dialog.open) dialog.close()
}

watch(palette.open, follow, { flush: 'post' })

onMounted(() => {
  if (palette.open.value) void follow(true)
})

function ask(text: string): void {
  stop()
  active.value = 0
  slots.value = []
  arrival.value = []
  pending.value = 0
  const trimmed = text.trim()
  if (trimmed === '') return
  const controller = new AbortController()
  const call = latest
  asking = controller
  pending.value = palette.sources.value.length
  void searchEach(trimmed, controller.signal, (index, slot) => {
    if (call !== latest) return
    const first = slots.value[index] === undefined
    const next = [...slots.value]
    next[index] = slot
    slots.value = next
    if (!first) return
    arrival.value = [...arrival.value, index]
    pending.value -= 1
  })
}

watch(query, ask)

// Search registers from a chunk of its own; a query typed before it arrived
// is asked again the moment a source joins, rather than left unanswered.
watch(palette.sources, () => ask(query.value))

watch(
  [active, found],
  () => {
    const row = list.value?.querySelector<HTMLElement>(`#palette-option-${active.value}`)
    marker.value =
      row === null || row === undefined
        ? null
        : { transform: `translateY(${row.offsetTop}px)`, height: `${row.offsetHeight}px` }
  },
  { flush: 'post' },
)

onBeforeUnmount(stop)

function choose(item: PaletteItem | undefined, destination?: PaletteAction): void {
  if (item === undefined) return
  palette.hide()
  ;(destination ?? item).run()
}

/** ←/→ and Tab move between a row's destinations; a row without them keeps the keys. */
function step(event: KeyboardEvent): number {
  if (activeActions.value.length < 2) return 0
  if (event.key === 'ArrowRight' || (event.key === 'Tab' && !event.shiftKey)) return 1
  if (event.key === 'ArrowLeft' || (event.key === 'Tab' && event.shiftKey)) return -1
  return 0
}

function onKeydown(event: KeyboardEvent): void {
  const last = found.value.length - 1
  const by = step(event)
  const count = activeActions.value.length
  if (by !== 0) action.value = (action.value + by + count) % count
  else if (event.key === 'ArrowDown') active.value = Math.min(last, active.value + 1)
  else if (event.key === 'ArrowUp') active.value = Math.max(0, active.value - 1)
  else if (event.key === 'Enter')
    choose(found.value[active.value], activeActions.value[action.value])
  else return
  event.preventDefault()
}

/**
 * The close event is queued, so it can arrive after ⌘K has already reopened
 * the dialog; a dialog that is open again has not been closed.
 */
function onClose(): void {
  if (element.value?.open !== true) palette.hide()
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === element.value) palette.hide()
}
</script>

<template>
  <dialog
    ref="element"
    class="palette mx-auto mt-palette-top w-palette overflow-hidden rounded-card border border-line-strong bg-surface-raised p-0 text-fg shadow-md backdrop:bg-backdrop open:animate-appear"
    aria-label="Find"
    data-testid="palette"
    @cancel.prevent="palette.hide()"
    @click="onBackdrop"
    @close="onClose"
  >
    <label class="flex h-12 items-center gap-2.5 border-b border-line px-3.5">
      <VIcon name="telescope" class="text-fg-muted" />
      <input
        ref="field"
        v-model="query"
        class="h-full flex-1 border-0 bg-transparent font-[inherit] text-md text-fg outline-none placeholder:text-fg-muted"
        placeholder="Find a table, request, operator"
        aria-label="Find"
        role="combobox"
        aria-autocomplete="list"
        aria-controls="palette-results"
        :aria-expanded="found.length > 0"
        v-bind="activeAttr"
        @keydown="onKeydown"
      />
      <span class="kbd">esc</span>
    </label>

    <div
      v-if="found.length > 0"
      id="palette-results"
      ref="list"
      class="relative max-h-80 overflow-y-auto p-1"
      role="listbox"
      aria-label="Results"
    >
      <span
        v-if="marker"
        class="marker pointer-events-none absolute inset-x-1 top-0 rounded-control bg-primary-wash"
        :style="marker"
        aria-hidden="true"
      />
      <section
        v-for="section in shown.sections"
        :key="section.group"
        role="group"
        :aria-label="section.group || 'Results'"
      >
        <h3 v-if="section.group" class="m-0 px-2.5 pt-2 pb-1 hud font-normal text-fg-muted">
          {{ section.group }}
        </h3>
        <div
          v-for="{ item, index } in section.items"
          :id="`palette-option-${index}`"
          :key="item.id"
          role="option"
          :aria-selected="index === active"
          class="option relative flex cursor-pointer items-center gap-2.5 rounded-control px-2.5 py-2 text-sm"
          :class="index === active ? 'text-fg' : 'text-fg-soft'"
          :style="{ animationDelay: `${Math.min(index, 10) * 18}ms` }"
          @mouseenter="active = index"
          @click="choose(item)"
        >
          <VIcon :name="item.icon" class="text-fg-muted" />
          <span class="truncate">{{ item.label }}</span>
          <span v-if="item.hint" class="ml-auto hud text-fg-muted">{{ item.hint }}</span>
          <span
            v-if="item.actions"
            class="actions flex flex-none gap-1"
            :class="item.hint ? '' : 'ml-auto'"
          >
            <button
              v-for="(one, destination) in item.actions"
              :id="optionId(index, destination)"
              :key="one.label"
              type="button"
              tabindex="-1"
              class="action cursor-pointer rounded-control border px-1.5 py-0.5 font-[inherit] text-2xs ease-colours"
              :class="
                index === active && destination === action
                  ? 'border-primary-soft bg-primary-wash text-primary'
                  : 'border-line bg-transparent text-fg-muted hover:text-fg'
              "
              :aria-label="one.describe"
              :data-active="index === active && destination === action"
              @click.stop="choose(item, one)"
            >
              {{ one.label }}
            </button>
          </span>
        </div>
      </section>
    </div>
    <p v-else class="empty m-0 px-3.5 py-6 text-center text-sm text-fg-muted">
      {{
        !connected
          ? 'Nothing is searchable here yet.'
          : query.trim() === ''
            ? 'Type to search.'
            : pending > 0
              ? 'Searching…'
              : `Nothing matches “${query.trim()}”.`
      }}
    </p>

    <p
      v-if="shown.statuses.length > 0"
      class="status m-0 border-t border-line px-3.5 py-1.5 hud text-fg-muted"
    >
      {{ shown.statuses.join(' · ') }}
    </p>
    <footer class="flex items-center gap-4 border-t border-line px-3.5 py-2 hud text-fg-muted">
      <span><span class="kbd">↑↓</span> move</span>
      <span><span class="kbd">↵</span> open</span>
      <span v-if="activeActions.length > 1"><span class="kbd">←→</span> destination</span>
      <span class="ml-auto"><span class="kbd">⌘K</span> toggle</span>
    </footer>
  </dialog>
</template>

<style scoped>
.marker {
  transition: transform var(--duration-band) var(--ease-out);
}

.option {
  animation: var(--animate-rise-in);
}
</style>
