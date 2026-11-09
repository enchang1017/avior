<script setup lang="ts">
import { computed } from 'vue'

import { useUtcClock } from '@/shared/clock'
import { usePalette } from '@/shared/palette'
import ThemeToggle from '@/shared/ThemeToggle.vue'
import AccountMenu from './AccountMenu.vue'
import { useSessionStore } from '@/stores/session'
import { useTargetStore } from '@/stores/target'
import { VBeacon, VIcon } from '@/components'

/**
 * The HUD over every page: where you are, what you are pointed at, which
 * deployment this is and what time the records are kept in.
 *
 * TARGET is the one readout that matters before a statement runs, so it sits
 * beside the title rather than in a corner: instance, database, table, in the
 * target colour. The search trigger opens the ⌘K palette.
 */
const props = defineProps<{ section: string; page: string; readOnly: boolean }>()

defineSlots<{ search?: () => unknown }>()

const session = useSessionStore()
const target = useTargetStore()
const palette = usePalette()
const clock = useUtcClock()

const aimed = computed(() =>
  target.database === null
    ? ''
    : [session.caller?.instance.name ?? '', target.database, target.table ?? '']
        .filter((part) => part !== '')
        .join(' / '),
)
</script>

<template>
  <header
    class="hud-bar flex h-hud flex-none items-center gap-4 border-b border-line bg-surface px-4"
  >
    <nav class="flex shrink-0 items-center gap-1.5" aria-label="Breadcrumb">
      <template v-if="props.section !== ''">
        <span class="truncate text-md text-fg-muted">{{ props.section }}</span>
        <VIcon name="chevron-right" size="sm" class="text-fg-faint" />
      </template>
      <span class="page truncate text-md font-semibold tracking-tight" aria-current="page">{{
        props.page
      }}</span>
    </nav>

    <span
      class="target hidden min-w-0 items-center gap-2 rounded-control border border-line px-2 py-1 lg:flex"
      v-bind="aimed === '' ? {} : { title: aimed }"
    >
      <span class="hud text-fg-muted">Target</span>
      <VIcon name="crosshair" size="sm" class="text-target" />
      <span v-if="aimed !== ''" class="aimed truncate font-mono text-xs text-fg-soft">{{
        aimed
      }}</span>
      <span v-else class="aimed hud text-fg-muted">no target</span>
    </span>

    <div class="search-slot ml-auto flex shrink-0">
      <slot name="search">
        <button
          type="button"
          class="find flex h-8 w-72 min-w-0 cursor-pointer items-center gap-2 rounded-control border border-line bg-surface-sunken px-2.5 font-[inherit] text-sm text-fg-muted hover:border-line-strong hover:text-fg-soft"
          aria-label="Find a table, request, operator"
          @click="palette.show()"
        >
          <VIcon name="telescope" />
          <span class="truncate">Find a table, request, operator</span>
          <span class="ml-auto kbd">⌘K</span>
        </button>
      </slot>
    </div>

    <VBeacon
      v-if="session.caller"
      :environment="session.caller.environment"
      :read-only="props.readOnly"
    />
    <span class="clock hud whitespace-nowrap text-fg-muted" :title="'Records are kept in UTC'"
      >UTC {{ clock }}</span
    >
    <ThemeToggle />
    <AccountMenu />
  </header>
</template>
