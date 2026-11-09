<script setup lang="ts">
import type { AuditScope } from './useAuditLog'
import { useAuditSky, type SkyLog } from './useAuditSky'
import { lazy } from '@/shared/lazy'

const loadSky = () => import('@/components/Data/VSkyline/VSkyline.vue')
const VSkyline = lazy(loadSky, 'Loading the sky', 'inline')
void loadSky()

const props = defineProps<{ scope: AuditScope; log: SkyLog }>()

const sky = useAuditSky(() => props.scope, props.log)
const { histogram, failure, span, label } = sky
</script>

<template>
  <div class="sky-strip rounded-card border border-line bg-surface px-3 py-2.5">
    <p v-if="failure" class="sky-failure m-0 text-xs text-danger">{{ failure }}</p>
    <VSkyline
      v-else-if="histogram"
      :buckets="histogram.buckets"
      :span="span"
      @brush="sky.brush"
      @clear="sky.clear"
    >
      <template #window>
        <span v-if="label" class="window flex items-center gap-1.5 text-primary">
          Window {{ label }}
          <button
            type="button"
            class="clear-window cursor-pointer rounded-sm border-0 bg-transparent px-1 font-mono text-3xs tracking-hud text-primary uppercase hover:text-fg"
            @click="sky.clear"
          >
            Clear
          </button>
        </span>
      </template>
    </VSkyline>
    <p v-else class="sky-placeholder m-0 font-mono text-3xs tracking-hud text-fg-muted uppercase">
      Sky · last 7 days
    </p>
  </div>
</template>
