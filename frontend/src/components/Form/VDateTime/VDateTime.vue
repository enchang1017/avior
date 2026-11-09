<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import VButton from '../VButton/VButton.vue'
import { controlBox } from '../controls'
import VIcon from '../../Media/VIcon/VIcon.vue'

/**
 * A date and a time, drawn by the app.
 *
 * `<input type="datetime-local">` hands the job to the operating system, which
 * answers with its own widget: a light panel in another font, sized for a phone,
 * sitting under a dark toolbar it knows nothing about. The same argument
 * VSelect was written for, and a stronger one — the native picker is far larger
 * and far louder than a filter control has any business being.
 *
 * The value is the string `datetime-local` uses, `YYYY-MM-DDTHH:mm`, so a
 * caller swaps the element for this and changes nothing else. Empty means none.
 */
const props = withDefaults(
  defineProps<{
    modelValue: string
    id?: string
    /** What the field is for, when it is not obvious from the label beside it. */
    placeholder?: string
    disabled?: boolean
  }>(),
  { id: '', placeholder: 'Any time', disabled: false },
)

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const open = ref(false)
const root = ref<HTMLDivElement | null>(null)

/*
 * The month on screen, which is not the same as the value.
 *
 * Somebody paging through months has not chosen anything yet, and resetting the
 * view to the selected date every time it changed would take the page back
 * under them.
 */
const viewing = ref(startOfMonth(parse(props.modelValue) ?? new Date()))

/** Parses the value, or null when it is empty or not a date. */
function parse(value: string): Date | null {
  if (value === '') return null
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

/** Renders a date the way `datetime-local` writes one, in local time. */
function render(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  )
}

function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

const selected = computed(() => parse(props.modelValue))

const shown = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
const monthLabel = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'long' })
const weekdayLabel = new Intl.DateTimeFormat(undefined, { weekday: 'short' })

const label = computed(() => {
  const date = selected.value
  return date === null ? props.placeholder : shown.format(date)
})

/**
 * Six weeks from the Sunday on or before the first of the month.
 *
 * Always six, so the panel does not change height as somebody pages through
 * months — a grid that grows a row under the pointer moves the day they were
 * about to click.
 */
const days = computed(() => {
  const first = viewing.value
  const start = new Date(first)
  start.setDate(1 - first.getDay())

  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start)
    date.setDate(start.getDate() + index)
    return date
  })
})

const weekdays = computed(() => days.value.slice(0, 7).map((date) => weekdayLabel.format(date)))

const hours = computed(() => (selected.value?.getHours() ?? 0).toString().padStart(2, '0'))
const minutes = computed(() => (selected.value?.getMinutes() ?? 0).toString().padStart(2, '0'))

function inMonth(date: Date): boolean {
  return date.getMonth() === viewing.value.getMonth()
}

function isToday(date: Date): boolean {
  return sameDay(date, new Date())
}

function isSelected(date: Date): boolean {
  const current = selected.value
  return current !== null && sameDay(date, current)
}

/*
 * The days either side of the month are shown rather than blank, so a week
 * reads as a week — and dimmed, so they are not mistaken for this month's.
 */
function dayClass(date: Date): string {
  const names = ['day', inMonth(date) ? '' : 'outside', isToday(date) ? 'today' : '']
  if (isSelected(date))
    return [...names, 'picked border-primary bg-primary text-on-primary'].join(' ')
  const edge = isToday(date) ? 'border-line-strong' : 'border-transparent'
  const text = inMonth(date) ? 'text-fg' : 'text-fg-muted'
  return [...names, edge, text, 'bg-transparent hover:bg-surface-sunken'].join(' ')
}

const clock = [controlBox, 'w-13 px-2 py-1 text-center font-mono text-md']

// Picking a day keeps whatever time was already chosen: somebody narrowing a
// range by a day should not have their hour reset to midnight under them.
function chooseDay(date: Date): void {
  const current = selected.value
  const next = new Date(date)
  next.setHours(current?.getHours() ?? 0, current?.getMinutes() ?? 0, 0, 0)
  emit('update:modelValue', render(next))
}

function chooseTime(part: 'hours' | 'minutes', raw: string): void {
  const value = Number(raw)
  if (!Number.isInteger(value)) return

  const next = new Date(selected.value ?? viewing.value)
  if (part === 'hours') next.setHours(Math.min(23, Math.max(0, value)))
  else next.setMinutes(Math.min(59, Math.max(0, value)))
  next.setSeconds(0, 0)
  emit('update:modelValue', render(next))
}

function step(months: number): void {
  const next = new Date(viewing.value)
  next.setMonth(next.getMonth() + months)
  viewing.value = next
}

function today(): void {
  const now = new Date()
  viewing.value = startOfMonth(now)
  now.setSeconds(0, 0)
  emit('update:modelValue', render(now))
}

function clear(): void {
  emit('update:modelValue', '')
}

// Opening on a value shows the month that value is in; opening on none shows
// this one. Either way the panel opens where the reader expects to be.
watch(open, (isOpen) => {
  if (!isOpen) return
  viewing.value = startOfMonth(selected.value ?? new Date())
})

function onPointerDown(event: MouseEvent): void {
  if (root.value?.contains(event.target as globalThis.Node) === true) return
  open.value = false
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && open.value) {
    event.stopPropagation()
    open.value = false
  }
}

onMounted(() => {
  window.addEventListener('mousedown', onPointerDown)
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('mousedown', onPointerDown)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="relative inline-flex min-w-0">
    <!-- The trigger is an input as far as the page is concerned: the same
         height, the same border, the same focus ring as the controls beside it. -->
    <button
      :id="props.id"
      class="trigger inline-flex w-full cursor-pointer items-center justify-between gap-2 px-2 py-1 text-left font-[inherit] text-md disabled:cursor-not-allowed disabled:text-fg-muted"
      :class="controlBox"
      type="button"
      :disabled="props.disabled"
      :aria-expanded="open"
      aria-haspopup="dialog"
      @click="open = !open"
    >
      <span
        class="shown overflow-hidden text-ellipsis whitespace-nowrap"
        :class="{ 'empty text-fg-muted': selected === null }"
        >{{ label }}</span
      >
      <VIcon name="calendar" class="text-sm text-fg-soft" />
    </button>

    <div
      v-if="open"
      class="panel absolute top-full left-0 z-20 mt-1 rounded-control border border-line-strong bg-surface p-2 shadow-md"
      role="dialog"
      aria-label="Choose a date and time"
    >
      <header class="head mb-1.5 flex items-center justify-between gap-1">
        <VButton
          size="sm"
          variant="bare"
          icon="chevron-left"
          aria-label="Previous month"
          @click="step(-1)"
        />
        <span class="month text-md font-semibold">{{ monthLabel.format(viewing) }}</span>
        <VButton
          size="sm"
          variant="bare"
          icon="chevron-right"
          aria-label="Next month"
          @click="step(1)"
        />
      </header>

      <div class="grid grid-cols-[repeat(7,--spacing(8))] gap-0.5" role="grid">
        <span
          v-for="name in weekdays"
          :key="name"
          class="grid h-6 place-items-center text-2xs font-semibold text-fg-soft"
          >{{ name }}</span
        >
        <button
          v-for="date in days"
          :key="date.toISOString()"
          class="h-8 cursor-pointer rounded-control border p-0 font-[inherit] text-sm leading-[inherit]"
          type="button"
          :class="dayClass(date)"
          :aria-pressed="isSelected(date)"
          @click="chooseDay(date)"
        >
          {{ date.getDate() }}
        </button>
      </div>

      <div class="mt-2 flex items-center gap-1">
        <label
          class="mr-auto text-xs font-semibold tracking-label text-fg-soft uppercase"
          :for="`${props.id}-hours`"
          >Time</label
        >
        <input
          :id="`${props.id}-hours`"
          :class="clock"
          type="number"
          min="0"
          max="23"
          :value="hours"
          @input="chooseTime('hours', ($event.target as HTMLInputElement).value)"
        />
        <span class="text-fg-soft">:</span>
        <input
          :id="`${props.id}-minutes`"
          :class="clock"
          type="number"
          min="0"
          max="59"
          :value="minutes"
          @input="chooseTime('minutes', ($event.target as HTMLInputElement).value)"
        />
      </div>

      <footer class="foot mt-2 flex items-center gap-1 border-t border-line pt-2">
        <VButton size="sm" variant="bare" @click="clear()">Clear</VButton>
        <VButton size="sm" variant="bare" @click="today()">Now</VButton>
        <VButton size="sm" class="ml-auto" @click="open = false">Done</VButton>
      </footer>
    </div>
  </div>
</template>
