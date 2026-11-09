<script setup lang="ts">
import { computed } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'
import VMenu from '../../Overlay/VMenu/VMenu.vue'
import type { CurrentUser } from '@/shared/api'

/**
 * The connection bar: engine, instance, database, table, and the deployment
 * they are on — the whole of where a statement will run, before it runs.
 *
 * The database is a picker: the caller's reachable databases (FEAT-058) and
 * nothing else, and choosing one points the current tab at it.
 *
 * It arms on production once a database is chosen: the bar takes the
 * production tint, because that is the moment the answer to "where does this
 * go" stops being academic. With nothing chosen it says what to do first.
 */
const props = withDefaults(
  defineProps<{
    instance: string
    environment: CurrentUser['environment']
    database: string | null
    table: string | null
    /** What may be picked. Empty draws the database as text, not a picker. */
    databases?: readonly string[]
  }>(),
  { databases: () => [] },
)

const emit = defineEmits<{ choose: [database: string] }>()

const chosen = computed(() => props.database !== null)
const armed = computed(() => chosen.value && props.environment === 'production')

const state = computed(() => {
  if (!chosen.value) return 'idle'
  return armed.value ? 'armed' : 'ready'
})

const looks = {
  armed: 'border-env-production-line bg-env-production-wash',
  ready: 'border-line bg-surface-sunken',
  idle: 'border-line bg-surface-sunken',
} as const
</script>

<template>
  <div
    class="risk flex h-7 min-w-0 items-center gap-2 rounded-control border px-2.5 text-xs"
    :class="[state, looks[state]]"
  >
    <span class="hud text-fg-muted">PostgreSQL</span>
    <nav class="flex min-w-0 items-center gap-1.5 font-mono" aria-label="Target">
      <span class="crumb instance truncate text-fg-soft">{{ props.instance }}</span>
      <VIcon name="chevron-right" size="xs" class="text-fg-faint" />
      <VMenu v-if="props.databases.length > 0" label="Choose a database" align="right">
        <template #trigger>
          <span
            class="picker flex items-center gap-1 text-fg hover:text-primary"
            :class="props.database ? '' : 'text-fg-muted'"
          >
            <span class="crumb truncate">{{ props.database ?? 'database' }}</span>
            <VIcon name="chevron-down" size="xs" />
          </span>
        </template>
        <button
          v-for="name in props.databases"
          :key="name"
          type="button"
          role="menuitemradio"
          :aria-checked="name === props.database"
          class="option flex w-full cursor-pointer items-center gap-2 rounded-control border-0 bg-transparent px-2.5 py-1.5 text-left font-mono text-sm text-fg hover:bg-surface-sunken"
          @click="emit('choose', name)"
        >
          <VIcon name="database" size="sm" class="text-fg-muted" />
          {{ name }}
          <VIcon
            v-if="name === props.database"
            name="check"
            size="sm"
            class="ml-auto text-primary"
          />
        </button>
      </VMenu>
      <span v-else-if="props.database" class="crumb truncate text-fg">{{ props.database }}</span>
      <template v-if="props.table">
        <VIcon name="chevron-right" size="xs" class="text-fg-faint" />
        <span class="crumb truncate text-target">{{ props.table }}</span>
      </template>
    </nav>
    <span
      v-if="chosen"
      class="env-name ml-1 hud"
      :class="armed ? 'text-env-production' : 'text-fg-muted'"
      >{{ props.environment }}</span
    >
    <span v-else class="hint ml-1 text-fg-muted">Pick a database on the left to begin.</span>
  </div>
</template>
