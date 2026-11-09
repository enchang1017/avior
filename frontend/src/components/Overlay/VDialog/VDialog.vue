<script setup lang="ts">
import { onBeforeUnmount, ref, useId, watch } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'

/**
 * A modal, on the platform's own `<dialog>`.
 *
 * Escape, the focus trap, the inert background and the top layer are all things
 * a hand-rolled overlay has to reimplement and usually reimplements wrongly.
 * The only thing added here is closing on a click outside, which `<dialog>`
 * deliberately does not do: the backdrop is the element's own pseudo-element, so
 * a click on it lands on the dialog itself and is told apart by where it fell.
 */
const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    /**
     * How it arrives. `appear` grows from a hair smaller; `fade` only fades,
     * for a dialog opened over and over while reading a page, where a moving
     * box is noise. Reduced motion turns both into a fade.
     */
    motion?: 'appear' | 'fade'
  }>(),
  { motion: 'appear' },
)
const emit = defineEmits<{ close: [] }>()

/**
 * `badge` sits beside the title and `status` at the header's far end, before
 * the close button; `footer` holds the dialog's acts under its body.
 */
defineSlots<{
  default?: () => unknown
  badge?: () => unknown
  status?: () => unknown
  footer?: () => unknown
}>()

const element = ref<HTMLDialogElement | null>(null)
/** The heading names the dialog, so assistive technology announces what opened. */
const headingId = useId()

watch(
  () => props.open,
  (open) => {
    const dialog = element.value
    if (dialog === null) return
    // showModal on an already-open dialog throws, and close on a closed one is
    // a no-op that still fires nothing. Both are reachable when a parent
    // re-renders, so neither is left to chance.
    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  },
  // immediate, because a dialog mounted already open would otherwise never be
  // shown: the watch has nothing to react to.
  { flush: 'post', immediate: true },
)

onBeforeUnmount(() => element.value?.close())

// Escape and the close button both reach here, so the parent's state follows
// however it was dismissed rather than only when the button was used.
function dismiss() {
  emit('close')
}

function onBackdrop(event: MouseEvent) {
  if (event.target === element.value) emit('close')
}
</script>

<template>
  <dialog
    ref="element"
    class="m-auto max-h-dialog-max w-dialog overflow-hidden rounded-card border border-line-strong bg-surface-raised p-0 text-fg shadow-sheet backdrop:bg-backdrop"
    :class="props.motion === 'fade' ? 'open:animate-fade' : 'open:animate-appear'"
    :aria-labelledby="headingId"
    @cancel.prevent="dismiss"
    @click="onBackdrop"
  >
    <header class="flex items-center justify-between gap-4 border-b border-line px-4 py-3">
      <div class="flex min-w-0 items-center gap-2">
        <h2 :id="headingId" class="title m-0 text-lg font-semibold">{{ props.title }}</h2>
        <slot name="badge" />
      </div>
      <div v-if="$slots.status" class="ml-auto flex items-center"><slot name="status" /></div>
      <button
        class="close inline-flex size-7 cursor-pointer items-center justify-center rounded-sm border border-transparent bg-transparent p-0 text-fg-soft hover:border-line-strong hover:text-fg"
        type="button"
        aria-label="Close"
        @click="dismiss"
      >
        <VIcon name="x" />
      </button>
    </header>
    <div class="max-h-dialog-body-max overflow-auto p-4">
      <slot />
    </div>
    <footer v-if="$slots.footer" class="flex justify-end gap-2 border-t border-line px-4 py-3">
      <slot name="footer" />
    </footer>
  </dialog>
</template>
