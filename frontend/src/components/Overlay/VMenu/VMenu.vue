<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

/**
 * A menu hung under the thing that opens it.
 *
 * Not VSelect. That control chooses a value and reports it, and everything it
 * does — a text field, a highlighted option, a value written back — is about
 * that. A menu's items navigate or act, there is nothing to choose, and giving
 * one a combobox's keyboard would be a control that lies about what it is.
 *
 * Not VDialog either, for the opposite reason: a dialog takes the whole screen's
 * attention and traps focus, which is right for a decision and wrong for a
 * three-item list somebody opened to look at.
 *
 * The trigger is a slot rather than a prop, because an avatar, a name and an
 * icon are all reasonable and none of them is this component's business.
 */
const props = withDefaults(
  defineProps<{ label?: string; align?: 'left' | 'right'; side?: 'below' | 'above' }>(),
  { label: 'Open the menu', align: 'right', side: 'below' },
)

defineSlots<{ trigger?: () => unknown; default?: () => unknown }>()

const open = ref(false)
const root = ref<HTMLDivElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)

function close(): void {
  open.value = false
}

// A click anywhere else closes it, including on a control that then does its
// own thing — so the menu never survives the page changing under it.
function onPointerDown(event: MouseEvent): void {
  if (root.value?.contains(event.target as globalThis.Node) === true) return
  close()
}

// Escape closes this and nothing else. stopPropagation because a dialog or a
// page behind it may also listen, and one key press closing two things reads as
// the first press having been dropped. Focus goes back to the trigger, since
// an item it was on is about to be removed.
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && open.value) {
    event.stopPropagation()
    close()
    trigger.value?.focus()
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
      ref="trigger"
      class="trigger inline-flex cursor-pointer items-center gap-1.5 border-none bg-transparent p-0 text-inherit [font:inherit]"
      type="button"
      :aria-label="props.label"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      <slot name="trigger" />
    </button>

    <!-- Closed on click rather than on each item, so an item is a link or a
         button and knows nothing about being in a menu. -->
    <div
      v-if="open"
      class="absolute z-30 min-w-56 animate-appear rounded-card border border-line-strong bg-surface-raised p-1 shadow-md"
      :class="[
        props.align === 'right' ? 'right-0' : 'left-0',
        props.side === 'above' ? 'bottom-full mb-1.5' : 'top-full mt-1.5',
      ]"
      role="menu"
      @click="close()"
    >
      <slot />
    </div>
  </div>
</template>
