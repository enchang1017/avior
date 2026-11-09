<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

/**
 * A panel that says more about the thing that opened it.
 *
 * VMenu's sibling: the same trigger and the same ways to close, but what opens
 * is something to read rather than a list of things to do, so a click inside it
 * does not close it and it is announced as a dialog, not a menu.
 */
const props = withDefaults(
  defineProps<{
    label: string
    align?: 'left' | 'right'
    side?: 'below' | 'above'
    /** Wide clears a padded bar the trigger sits in, so the panel never covers it. */
    offset?: 'near' | 'wide'
  }>(),
  { align: 'left', side: 'below', offset: 'near' },
)

const gap = {
  below: { near: 'top-full mt-1.5', wide: 'top-full mt-3' },
  above: { near: 'bottom-full mb-1.5', wide: 'bottom-full mb-3' },
} as const

defineSlots<{ trigger?: () => unknown; default?: () => unknown }>()

const open = ref(false)
const root = ref<HTMLDivElement | null>(null)

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
  <div ref="root" class="relative inline-flex">
    <button
      class="trigger inline-flex cursor-pointer items-center gap-1.5 border-none bg-transparent p-0 text-inherit [font:inherit]"
      type="button"
      :aria-expanded="open"
      aria-haspopup="dialog"
      @click="open = !open"
    >
      <slot name="trigger" />
    </button>
    <div
      v-if="open"
      class="popover absolute z-30 w-max max-w-md animate-appear rounded-card border border-line-strong bg-surface-raised p-3 text-sm shadow-md"
      :class="[props.align === 'right' ? 'right-0' : 'left-0', gap[props.side][props.offset]]"
      role="dialog"
      :aria-label="props.label"
    >
      <slot />
    </div>
  </div>
</template>
