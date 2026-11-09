<script setup lang="ts">
import VIcon from '../../Media/VIcon/VIcon.vue'
import type { IconName } from '../../Media/VIcon/icons'
import VToolbar from '../VToolbar/VToolbar.vue'

/**
 * The bar every page opens with, in one shape: an icon tile, the title, how
 * many there are, at most one short line under it, and the page's actions.
 *
 * No explanatory paragraph. What a page is for is its title; what a rule does
 * is said where the rule refuses something, not in prose over every list.
 */
const props = withDefaults(
  defineProps<{
    icon: IconName
    title?: string
    /** How many the page holds, when it is a list. Absent says nothing. */
    count?: number | null
    subtitle?: string
    busy?: boolean
    /** What the page is doing while busy. "Reading" unless it is doing more. */
    busyLabel?: string
  }>(),
  { title: '', count: null, subtitle: '', busy: false, busyLabel: 'Reading' },
)

defineSlots<{ title?: () => unknown; default?: () => unknown }>()
</script>

<template>
  <VToolbar :busy="props.busy" :busy-label="props.busyLabel">
    <template #lead>
      <div class="flex min-w-0 items-center gap-3">
        <span
          class="tile grid size-8 flex-none place-items-center rounded-control border border-line bg-surface-sunken text-fg-soft"
        >
          <VIcon :name="props.icon" />
        </span>
        <div class="flex min-w-0 flex-col">
          <div class="flex min-w-0 items-baseline gap-2">
            <slot name="title">
              <h1 class="title m-0 truncate text-xl leading-tight font-semibold tracking-tight">
                {{ props.title }}
              </h1>
            </slot>
            <span v-if="props.count !== null" class="count font-mono text-sm text-fg-muted">{{
              props.count
            }}</span>
          </div>
          <p v-if="props.subtitle !== ''" class="subtitle m-0 truncate text-xs text-fg-muted">
            {{ props.subtitle }}
          </p>
        </div>
      </div>
    </template>

    <slot />
  </VToolbar>
</template>
