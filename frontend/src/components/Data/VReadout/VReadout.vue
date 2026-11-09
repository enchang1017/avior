<script setup lang="ts">
/**
 * An instrument readout: `ALLOWED · ROWS 3 · ELAPSED 8 MS`, in the HUD's
 * monospace capitals. Each part is a key and a value; the first may carry a
 * tone, which is the verdict, and every value is set in the ink so it reads
 * apart from its key.
 */
export type ReadoutPart = { key?: string; value: string; tone?: 'ok' | 'danger' | 'primary' }

const props = defineProps<{ parts: readonly ReadoutPart[]; label: string }>()

const tones = { ok: 'text-ok', danger: 'text-danger', primary: 'text-primary' } as const
</script>

<template>
  <p
    class="readout m-0 flex min-w-0 items-center gap-1.5 hud text-fg-muted"
    :aria-label="props.label"
  >
    <template v-for="(part, index) in props.parts" :key="index">
      <span v-if="index > 0" class="text-fg-faint" aria-hidden="true">·</span>
      <span class="part truncate whitespace-nowrap">
        <template v-if="part.key">{{ `${part.key} ` }}</template>
        <span :class="part.tone ? [tones[part.tone], 'font-semibold'] : 'text-fg'">{{
          part.value
        }}</span>
      </span>
    </template>
  </p>
</template>
