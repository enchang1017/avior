<script setup lang="ts">
/**
 * A checkbox that stands in a row of controls.
 *
 * It is one control tall and sits at the bottom of the row, so its box shares
 * the vertical centre of the selects and text boxes beside it. A bare label
 * centred on the row lined up with their captions instead, because a field is
 * its caption and its box stacked.
 *
 * Attributes pass through to the input itself, as VInput's do.
 */
defineOptions({ inheritAttrs: false })

const model = defineModel<boolean>({ default: false })

function onChange(event: Event) {
  model.value = (event.target as HTMLInputElement).checked
}
</script>

<template>
  <label class="check inline-flex h-control shrink-0 items-center gap-2 self-end text-sm text-fg">
    <input v-bind="$attrs" type="checkbox" :checked="model" @change="onChange" />
    <slot />
  </label>
</template>
