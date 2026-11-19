<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, onUpdated, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    id: string
    anchor: HTMLElement | null
    options: readonly (string | { value: string; label: string })[]
    value: string
    active: number
    custom?: boolean
    clearLabel?: string
  }>(),
  { custom: false, clearLabel: '' },
)
const emit = defineEmits<{ close: []; choose: [value: string] }>()
const options = computed(() =>
  props.options.map((option) =>
    typeof option === 'string' ? { value: option, label: option } : option,
  ),
)
const panel = ref<HTMLElement | null>(null)
let place: (() => void) | undefined
let stop: (() => void) | undefined
let frame = 0

// Keep native listbox semantics and inherited theme, but use the browser's top
// layer so a dialog's scroll container cannot clip the options.
onMounted(() => {
  const list = panel.value
  const anchor = props.anchor
  if (!list || !anchor || typeof list.showPopover !== 'function') return
  const dialog = anchor.closest('dialog')
  if (dialog && !dialog.open) {
    emit('close')
    return
  }
  const style = getComputedStyle(list)
  const gap = parseFloat(style.marginTop) || 0
  const cap = parseFloat(style.maxHeight) || window.innerHeight
  const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth)
  Object.assign(list.style, { position: 'fixed', inset: 'auto', margin: '0' })
  list.popover = 'manual'
  list.showPopover()
  const update = () => {
    const box = anchor.getBoundingClientRect()
    const width = Math.max(0, Math.min(box.width, window.innerWidth - 2 * gap))
    const preferred = Math.min(cap, list.scrollHeight + border)
    const below = window.innerHeight - box.bottom - gap
    const above = box.top - gap
    const down = below >= preferred || below >= above
    const height = Math.max(0, Math.min(preferred, down ? below : above))
    Object.assign(list.style, {
      left: Math.max(gap, Math.min(box.left, window.innerWidth - width - gap)) + 'px',
      top: Math.max(gap, down ? box.bottom + gap : box.top - height - gap) + 'px',
      maxHeight: height + 'px',
      width: width + 'px',
    })
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      const active =
        list.querySelector<HTMLElement>('.active') ?? list.querySelector<HTMLElement>('.chosen')
      if (!active) return
      if (active.offsetTop < list.scrollTop) list.scrollTop = active.offsetTop
      else if (active.offsetTop + active.offsetHeight > list.scrollTop + list.clientHeight)
        list.scrollTop = active.offsetTop + active.offsetHeight - list.clientHeight
    })
  }
  place = update
  const scroll = (event: Event) => {
    if (event.target !== list) update()
  }
  let live = true
  const close = () => {
    if (live) emit('close')
  }
  const visible = new IntersectionObserver(([entry]) => {
    if (entry && !entry.isIntersecting) close()
  })
  visible.observe(anchor)
  place()
  window.addEventListener('resize', update)
  window.addEventListener('scroll', scroll, true)
  dialog?.addEventListener('close', close)
  stop = () => {
    live = false
    cancelAnimationFrame(frame)
    visible.disconnect()
    window.removeEventListener('resize', update)
    window.removeEventListener('scroll', scroll, true)
    dialog?.removeEventListener('close', close)
    if (list.matches(':popover-open')) list.hidePopover()
  }
})
onUpdated(() => place?.())
onBeforeUnmount(() => stop?.())

type Option = {
  /** A value, the entry that clears the choice, or the note that nothing matched. */
  kind: 'value' | 'clear' | 'none'
  active?: boolean
  chosen?: boolean
  /** Values are identifiers in a face that tells `l` from `1`. */
  mono?: boolean
  /** The note is a sentence, so it may wrap. */
  wrap?: boolean
}

function optionColour(option: Option): string {
  if (option.kind === 'none') return 'text-fg-muted'
  // The clearing entry is not one of the values, so it does not read as one.
  if (option.kind === 'clear') return 'text-fg-muted hover:bg-primary-wash'
  return option.active === true
    ? 'bg-primary-wash text-primary'
    : 'hover:bg-primary-wash hover:text-primary'
}

/** One entry in the list. */
function optionClass(option: Option): string {
  return [
    'option overflow-hidden rounded-control px-2 py-1.25 text-sm text-ellipsis',
    option.kind === 'none' && option.wrap === true ? 'whitespace-normal' : 'whitespace-nowrap',
    option.kind === 'none' ? 'none cursor-default' : 'cursor-pointer',
    option.kind === 'clear' ? 'clear' : '',
    option.kind === 'value' && option.mono === true ? 'font-mono' : '',
    option.active === true ? 'active' : '',
    option.chosen === true ? 'chosen font-bold' : '',
    optionColour(option),
  ]
    .filter(Boolean)
    .join(' ')
}
</script>

<template>
  <ul
    :id="props.id"
    ref="panel"
    class="list absolute top-full right-0 left-0 z-20 m-0 mt-1 max-h-64 animate-appear list-none overflow-y-auto rounded-card border border-line-strong bg-surface-raised p-0.75 text-fg shadow-md"
    role="listbox"
  >
    <li
      v-if="props.custom && props.value !== ''"
      :class="optionClass({ kind: 'clear' })"
      role="option"
      :aria-selected="false"
      @mousedown.prevent="emit('choose', '')"
    >
      {{ props.clearLabel }}
    </li>
    <li
      v-for="(option, index) in options"
      :key="option.value"
      :class="
        optionClass({
          kind: 'value',
          mono: props.custom,
          active: index === props.active,
          chosen: option.value === props.value,
        })
      "
      role="option"
      :aria-selected="option.value === props.value"
      @mousedown.prevent="emit('choose', option.value)"
    >
      {{ option.label }}
    </li>
    <li v-if="!options.length" :class="optionClass({ kind: 'none', wrap: props.custom })">
      {{
        props.custom ? 'Nothing here matches. It will still be asked for.' : 'Nothing here matches.'
      }}
    </li>
  </ul>
</template>
