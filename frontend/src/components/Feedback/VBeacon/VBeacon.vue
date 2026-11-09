<script setup lang="ts">
import { computed } from 'vue'

import type { CurrentUser } from '@/shared/api'

/**
 * Which deployment this is, on every page.
 *
 * Two Aviors open at once is the ordinary case, not an edge one, and the tab
 * somebody switched to to check something is where the question is asked.
 *
 * Production is a beacon: a red pill with a sweep turning inside a ring and a
 * slow pulse around it, the only thing on the page that moves all day. Staging
 * is a quiet blue pill and anything else is neutral, because a signal loud on
 * every deployment is one nobody reads on the deployment where it matters. The
 * sweep and the pulse are transform and opacity only, and stand still under
 * reduced motion.
 */
const props = withDefaults(
  defineProps<{
    environment: CurrentUser['environment']
    /** The query console only reads, and says so beside the deployment's name. */
    readOnly?: boolean
  }>(),
  { readOnly: false },
)

const shown = computed(() => props.environment.toUpperCase())
const production = computed(() => props.environment === 'production')

const tones: Partial<Record<CurrentUser['environment'], string>> = {
  production: 'border-env-production-line bg-env-production-wash text-env-production',
  staging: 'border-env-staging-line bg-env-staging-wash text-env-staging',
}
</script>

<template>
  <span
    class="env relative inline-flex h-7 items-center gap-2 rounded-control border px-2.5 whitespace-nowrap"
    :class="[
      props.environment,
      tones[props.environment] ?? 'border-env-local-line bg-env-local-wash text-env-local',
      production ? 'pl-1.5' : '',
    ]"
    :title="`This is the ${props.environment} deployment`"
  >
    <span
      v-if="production"
      class="beacon-pulse pointer-events-none absolute inset-0 animate-beacon-pulse rounded-control border border-env-production opacity-0"
      aria-hidden="true"
    />
    <span
      v-if="production"
      class="relative grid size-4.5 place-items-center overflow-hidden rounded-pill border border-env-production-line"
      aria-hidden="true"
    >
      <span class="absolute inset-0 animate-beacon-sweep beacon-sweep" />
      <span class="relative size-1.5 rounded-pill bg-env-production" />
    </span>
    <span v-else class="size-1.5 rounded-pill bg-current" aria-hidden="true" />
    <span class="hud font-semibold">{{ shown }}</span>
    <span v-if="production && props.readOnly" class="read-only hud opacity-80">· READ-ONLY</span>
  </span>
</template>
