<script setup lang="ts">
const model = defineModel<string>({ required: true })
function input(event: Event): void {
  model.value = (event.target as HTMLTextAreaElement).value
}
</script>
<template>
  <textarea
    class="w-full rounded-control border border-line-strong bg-surface-sunken px-2 py-1 font-mono text-sm text-fg outline-none focus:border-primary disabled:opacity-50"
    :value="model"
    @input="input"
  />
</template>
