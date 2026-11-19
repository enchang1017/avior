<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { isLosslessNumber } from 'lossless-json'
import { VButton, VInput, VTabs } from '@/components'
import { lazy } from '@/shared/lazy'
import { exactJSON, jsonDocument, arrayDocument, pointer, selectedJSON } from './jsonDocument'

const VJsonCode = lazy(() => import('./VJsonCode.vue'), 'Loading JSON code editor')
const props = withDefaults(defineProps<{ value: string; readOnly?: boolean; array?: boolean }>(), {
  readOnly: true,
  array: false,
})
const emit = defineEmits<{ 'update:value': [value: string]; valid: [valid: boolean] }>()
const mode = ref(props.readOnly ? 'tree' : 'code')
const status = ref('')
const path = ref<(string | number)[] | null>(null)
const search = ref('')
const expanded = ref(new Set(['']))
const document = computed(() =>
  props.array ? arrayDocument(props.value) : jsonDocument(props.value),
)
const code = computed(() =>
  (props.readOnly || props.array) && document.value.valid ? document.value.formatted : props.value,
)
type CodeHandle = { currentText: () => string }
let editor: CodeHandle | null = null
function captureEditor(instance: unknown): void {
  editor = instance as CodeHandle | null
}
watch(document, (value) => emit('valid', value.valid), { immediate: true })
watch(
  () => props.value,
  () => {
    path.value = null
    status.value = ''
  },
)

type Field = {
  path: (string | number)[]
  key: string
  label: string
  depth: number
  value: string
  branch: boolean
}
function branch(value: unknown): value is Record<string, unknown> | unknown[] {
  return value !== null && typeof value === 'object' && !isLosslessNumber(value)
}
function preview(value: unknown): string {
  if (branch(value))
    return Array.isArray(value)
      ? `Array (${value.length})`
      : `Object (${Object.keys(value).length})`
  return exactJSON.stringify(value) ?? ''
}
// Bound the drawn tree, including deeply nested or very wide JSON documents.
const fields = computed(() => {
  const data = document.value
  if (!data.valid) return []
  const found: Field[] = []
  const query = search.value.trim().toLowerCase()
  const pending = [{ value: data.value, path: [] as (string | number)[], label: '$', depth: 0 }]
  let visited = 0
  while (pending.length && found.length < 500 && visited < 10000) {
    const item = pending.pop()
    if (!item) break
    visited++
    const key = pointer(item.path)
    const nested = branch(item.value)
    const text = preview(item.value)
    if (!query || (key + ' ' + text).toLowerCase().includes(query))
      found.push({
        path: item.path,
        key,
        label: item.label,
        depth: item.depth,
        value: text,
        branch: nested,
      })
    if (branch(item.value) && item.depth < 64 && (query || expanded.value.has(key))) {
      const entries = Object.entries(item.value).slice(0, 500)
      for (const [name, value] of entries.reverse())
        pending.push({
          value,
          path: [...item.path, Array.isArray(item.value) ? Number(name) : name],
          label: name,
          depth: item.depth + 1,
        })
    }
  }
  return found
})
function toggle(field: Field): void {
  if (expanded.value.has(field.key)) expanded.value.delete(field.key)
  else expanded.value.add(field.key)
  path.value = field.path
}
function currentValue(): { text: string; valid: boolean } {
  const text = mode.value === 'code' && editor ? editor.currentText() : props.value
  return { text, valid: jsonDocument(text).valid }
}
defineExpose({ currentValue })
async function copy(kind: 'formatted' | 'path' | 'selected'): Promise<void> {
  const data = document.value
  if (!data.valid) return
  const text =
    kind === 'formatted'
      ? data.formatted
      : kind === 'path'
        ? pointer(path.value ?? [])
        : selectedJSON(data.value, path.value ?? [])
  try {
    await navigator.clipboard.writeText(text)
    status.value = kind === 'path' ? 'JSON Pointer copied.' : 'Copied.'
  } catch {
    status.value = 'Clipboard unavailable. Select the content and copy it.'
  }
}
</script>
<template>
  <div class="min-w-0">
    <p v-if="props.array" class="mt-0 text-xs text-fg-muted">
      Array shown as JSON. Copy original preserves PostgreSQL syntax and array bounds.
    </p>
    <div class="flex flex-wrap items-center gap-2 pb-2">
      <VTabs
        v-model="mode"
        label="JSON view"
        :tabs="[
          { id: 'tree', label: 'Tree' },
          { id: 'code', label: 'Code' },
        ]"
      />
      <VButton size="sm" :disabled="!document.valid" @click="copy('formatted')">
        <span>{{ props.array ? 'Copy as JSON' : 'Copy formatted' }}</span>
      </VButton>
      <VButton size="sm" :disabled="path === null || !document.valid" @click="copy('selected')">
        <span>Copy selection</span>
      </VButton>
      <VButton size="sm" :disabled="path === null || !document.valid" @click="copy('path')">
        <span>Copy path</span>
      </VButton>
    </div>
    <p v-if="!document.valid" class="m-0 pb-2 text-xs text-fg-muted">
      Invalid JSON. The original value is preserved; use Code to correct it.
    </p>
    <VJsonCode
      v-if="mode === 'code'"
      :ref="captureEditor"
      :value="code"
      :read-only="props.readOnly || props.array"
      @update:value="emit('update:value', $event)"
    />
    <template v-else-if="document.valid">
      <VInput
        v-model="search"
        aria-label="Search JSON fields"
        placeholder="Search keys or values"
        size="sm"
      />
      <div
        class="mt-2 h-80 overflow-auto border border-line bg-surface-sunken p-2 font-mono text-sm"
        role="region"
        aria-label="JSON fields"
      >
        <div
          v-for="field in fields"
          :key="field.key"
          class="flex min-w-max items-start gap-1"
          :style="{ paddingLeft: 'calc(var(--spacing) * ' + Math.min(field.depth, 12) * 3 + ')' }"
          :class="{ 'bg-primary-wash': pointer(path ?? []) === field.key && path !== null }"
        >
          <VButton
            v-if="field.branch"
            size="sm"
            variant="bare"
            :icon="expanded.has(field.key) ? 'chevron-down' : 'chevron-right'"
            :aria-expanded="expanded.has(field.key)"
            :aria-label="
              (expanded.has(field.key) ? 'Collapse ' : 'Expand ') + (field.key || 'root')
            "
            @click="toggle(field)"
          >
            <span>{{ field.label }}: {{ field.value }}</span>
          </VButton>
          <VButton v-else size="sm" variant="bare" :title="field.key" @click="path = field.path">
            <span class="text-primary">{{ field.label }}:</span
            ><span class="max-w-128 break-words whitespace-pre-wrap">{{ field.value }}</span>
          </VButton>
        </div>
        <p v-if="fields.length === 0" class="text-fg-muted">No matching fields.</p>
      </div>
      <p class="m-0 pt-2 text-xs text-fg-muted">
        Tree shows up to 500 fields and 64 levels. Code contains the full document{{
          !props.readOnly && !props.array ? ' and supports editing' : ''
        }}.
      </p>
    </template>
    <pre v-else class="overflow-auto font-mono text-sm whitespace-pre-wrap">{{ props.value }}</pre>
    <p class="m-0 pt-2 text-xs text-fg-muted" role="status">
      {{
        status ||
        (path === null
          ? 'Select a field to copy its value or JSON Pointer.'
          : 'Selected: ' + (pointer(path) || 'root'))
      }}
    </p>
  </div>
</template>
