<script setup lang="ts">
import type { GuardRule } from './guardRules'
import { VIcon, VPopover } from '@/components'

/**
 * "Guarded · N rules": the rules a statement is held to, one click away.
 *
 * They were a sentence over the editor on every page load, read once and then
 * never again. Folded into a chip they are still where the operator is working,
 * and the rule names are the ones a refusal will quote.
 */
const props = defineProps<{ rules: readonly GuardRule[] }>()
</script>

<template>
  <VPopover label="What the guard checks">
    <template #trigger>
      <span
        class="guard-chip flex h-7 items-center gap-1.5 rounded-control border border-line px-2 whitespace-nowrap text-fg-soft hover:border-line-strong hover:text-fg"
      >
        <VIcon name="shield-check" size="sm" class="text-ok" />
        <span class="hud">Guarded · {{ props.rules.length }} rules</span>
      </span>
    </template>
    <ul class="m-0 flex list-none flex-col gap-2 p-0">
      <li v-for="rule in props.rules" :key="rule.rule" class="flex flex-col gap-0.5">
        <span class="font-mono text-xs text-fg-muted">{{ rule.rule }}</span>
        <span class="text-sm text-fg">{{ rule.asks }}</span>
      </li>
    </ul>
  </VPopover>
</template>
