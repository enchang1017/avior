<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { VButton, VInput } from '@/components'
// Dense grid editing keeps the row height stable, uses native focus and the
// existing inset field, and leaves submission to the workspace review action.
const props = defineProps<{
  value: string | null
  nullable: boolean
  error: string
  label: string
}>()
const emit = defineEmits<{ value: [value: string | null]; save: [direction: number]; cancel: [] }>()
const host = ref<HTMLElement | null>(null)
const remembered = ref(props.value ?? '')
function input(value: string | number | undefined): void {
  remembered.value = String(value ?? '')
  emit('value', String(value ?? ''))
}
function nullValue(): void {
  emit('value', props.value === null ? remembered.value : null)
}
const composing = ref(false)
function key(event: KeyboardEvent): void {
  event.stopPropagation()
  if (composing.value || event.isComposing || event.keyCode === 229) return
  if (event.key === 'Enter' && event.altKey && props.nullable) {
    event.preventDefault()
    nullValue()
    return
  }
  if (event.key === 'Enter' && event.target instanceof Element && event.target.closest('button'))
    return
  if (event.key === 'Enter') {
    event.preventDefault()
    emit('save', 0)
  } else if (event.key === 'Escape') {
    event.preventDefault()
    emit('cancel')
  } else if (event.key === 'Tab') {
    event.preventDefault()
    emit('save', event.shiftKey ? -1 : 1)
  }
}
onMounted(() => host.value?.querySelector('input')?.focus())
</script>
<template>
  <div
    ref="host"
    class="min-w-40"
    @keydown="key"
    @compositionstart="composing = true"
    @compositionend="composing = false"
  >
    <div class="flex items-center gap-1">
      <VInput
        :model-value="props.value ?? ''"
        size="sm"
        :aria-label="'Edit ' + props.label"
        :placeholder="props.value === null ? 'NULL' : 'Value'"
        :aria-invalid="Boolean(props.error)"
        @update:model-value="input"
      />
      <VButton
        v-if="props.nullable"
        size="sm"
        variant="bare"
        :aria-pressed="props.value === null"
        aria-keyshortcuts="Alt+Enter"
        title="Toggle NULL (Alt+Enter); an empty string remains a separate value"
        @click="nullValue"
      >
        <span>NULL</span>
      </VButton>
    </div>
    <span v-if="props.error" class="sr-only" role="alert">{{ props.error }}</span>
  </div>
</template>
