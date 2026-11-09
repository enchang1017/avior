<script setup lang="ts">
defineProps<{ pressed: boolean }>()
defineEmits<{ toggle: [] }>()
defineSlots<{ default?: () => unknown }>()
</script>

<template>
  <button
    type="button"
    class="chip inline-flex h-control-sm cursor-pointer items-center gap-1 rounded-pill border px-2.5 font-[inherit] text-xs whitespace-nowrap ease-colours"
    :class="
      pressed
        ? 'border-primary-soft bg-primary-wash text-primary'
        : 'border-line bg-transparent text-fg-muted hover:text-fg'
    "
    :aria-pressed="pressed"
    @click="$emit('toggle')"
  >
    <slot />
  </button>
</template>
