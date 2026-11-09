<script setup lang="ts">
/**
 * A labelled control in a filter bar.
 *
 * `wide` is for the ones holding a qualified name; everything else gets the
 * width a database or a state needs and no more. A filter stretched across the
 * window reads as the page's main input, which it is not.
 */
withDefaults(defineProps<{ label: string; for: string; wide?: boolean }>(), { wide: false })
</script>

<template>
  <div
    class="field flex min-w-0 grow-0 flex-col gap-1"
    :class="wide ? 'shrink basis-88' : 'shrink-0 basis-48'"
  >
    <label :for="$props.for" class="eyebrow">{{ label }}</label>
    <slot />
  </div>
</template>
