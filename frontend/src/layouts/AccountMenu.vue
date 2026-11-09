<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'

import { useSessionStore } from '@/stores/session'
import { VMenu } from '@/components'

/**
 * The signed-in operator's initial at the right end of the HUD, where it stays
 * whether the rail is folded or not. What it opens is fetched once the shell
 * has mounted, so it is there by the first click without weighing on the
 * shell's first load (ADR 0009 P1).
 */
const session = useSessionStore()

const loadPanel = () => import('./AccountMenuPanel.vue')
const AccountMenuPanel = defineAsyncComponent(loadPanel)

const initial = computed(() => (session.caller?.username ?? '?').slice(0, 1).toUpperCase())
</script>

<template>
  <VMenu label="Your account" align="right" side="below">
    <template #trigger>
      <span
        class="grid size-8 place-items-center rounded-pill border border-line-strong font-mono text-xs text-fg-soft hover:border-primary hover:text-fg"
        @pointerenter="loadPanel"
        >{{ initial }}</span
      >
    </template>
    <AccountMenuPanel />
  </VMenu>
</template>
