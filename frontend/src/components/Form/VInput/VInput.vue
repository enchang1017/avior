<script setup lang="ts">
/**
 * A box somebody types into.
 *
 * Twenty-three of these were a bare `<input class="input">`, and the class was
 * the whole of what made one look like the others — which is why it kept being
 * forgotten. The element is the component now, so there is nothing to forget.
 *
 * Every attribute passes through to the input itself: `id`, `type`,
 * `placeholder`, `min`, `autocomplete` and the ARIA ones are the caller's, and
 * `v-model.number` works as it does on the element.
 */
const model = defineModel<string | number>({ default: '' })

const props = withDefaults(
  defineProps<{
    /** Matches VButton's, so a row of controls is one height. */
    size?: 'md' | 'sm'
  }>(),
  { size: 'md' },
)

function onInput(event: Event) {
  model.value = (event.target as HTMLInputElement).value
}
</script>

<template>
  <input
    class="input h-control w-full rounded-control border border-line-strong bg-surface-sunken px-2 font-[inherit] text-fg ease-colours placeholder:text-fg-muted hover:border-fg-muted focus:border-primary focus:outline-none"
    :class="props.size === 'sm' ? 'py-0.75 text-sm' : 'py-1 text-md'"
    :value="model"
    @input="onInput"
  />
</template>
