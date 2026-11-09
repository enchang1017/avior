<script setup lang="ts">
import { computed, onBeforeUnmount, useId, watch } from 'vue'

import { sceneHeight, sceneWidth } from '../VLineageChart/types'
import { galaxyLayout, type LinkInput, type SchemaInput } from './galaxy'

/**
 * A database at the scale of its schemas: each a nebula as large as the
 * objects it holds, the edges between two schemas one line as thick as they
 * are many, and no label but the schemas' names.
 */
const props = withDefaults(
  defineProps<{
    schemas: SchemaInput[]
    links: LinkInput[]
    summary: string
    /** The schema the camera flies to, by transform alone. */
    flyTo?: string | null
  }>(),
  { flyTo: null },
)
const emit = defineEmits<{ pick: [schema: string]; flown: [] }>()

const nebula = useId()
const viewBox = `${-sceneWidth / 2} ${-sceneHeight / 2} ${sceneWidth} ${sceneHeight}`
const layout = computed(() => galaxyLayout(props.schemas, props.links))

/** Close on the schema flown to: it comes to the centre and fills the view. */
const zoom = 4
const camera = computed(() => {
  const to = layout.value.clusters.find((cluster) => cluster.schema === props.flyTo)
  if (to === undefined) return undefined
  const x = (-to.x / sceneWidth) * 100
  const y = (-to.y / sceneHeight) * 100
  return { transform: `scale(${zoom}) translate(${x}%, ${y}%)` }
})

/**
 * The flight lasts --duration-fly, and it is none under reduced motion. The
 * timer is the end, not transitionend: a transition that never starts, as
 * under reduced motion, never ends either.
 */
function flightMs(): number {
  const value = getComputedStyle(document.documentElement).getPropertyValue('--duration-fly')
  const ms = Number.parseFloat(value)
  return Number.isFinite(ms) ? ms : 0
}
let landing: ReturnType<typeof setTimeout> | undefined
watch(
  () => props.flyTo,
  (to) => {
    clearTimeout(landing)
    if (to !== null) landing = setTimeout(() => emit('flown'), flightMs())
  },
  { immediate: true },
)
onBeforeUnmount(() => clearTimeout(landing))
</script>

<template>
  <figure class="m-0 flex min-w-0 flex-col overflow-hidden">
    <div
      class="camera origin-center transition-transform duration-(--duration-fly) ease-out will-change-transform"
      :style="camera"
    >
      <svg
        role="img"
        :aria-label="summary"
        :viewBox="viewBox"
        class="block h-auto w-full font-mono select-none"
        data-testid="lineage-galaxy"
      >
        <defs>
          <radialGradient :id="nebula">
            <stop offset="0" class="nebula-core" />
            <stop offset="1" class="nebula-edge" />
          </radialGradient>
        </defs>
        <g class="fill-none" aria-hidden="true">
          <line
            v-for="link in layout.links"
            :key="link.key"
            data-schema-link
            :data-link="link.key"
            :x1="link.x1"
            :y1="link.y1"
            :x2="link.x2"
            :y2="link.y2"
            :stroke-width="link.width"
            class="stroke-chart-edge-near"
            stroke-opacity="0.6"
            stroke-linecap="round"
            vector-effect="non-scaling-stroke"
          />
        </g>
        <g
          v-for="cluster in layout.clusters"
          :key="cluster.schema"
          class="cluster cursor-pointer"
          data-cluster
          :data-schema="cluster.schema"
          :data-objects="cluster.objects"
          @click="emit('pick', cluster.schema)"
        >
          <circle :cx="cluster.x" :cy="cluster.y" :r="cluster.r" :fill="`url(#${nebula})`" />
          <circle :cx="cluster.x" :cy="cluster.y" r="3" class="fill-star-view" />
          <text
            :x="cluster.label.x"
            :y="cluster.label.y"
            :text-anchor="cluster.label.anchor"
            class="fill-fg-soft text-xs"
          >
            {{ cluster.schema }}
          </text>
        </g>
      </svg>
    </div>
  </figure>
</template>

<style scoped>
.nebula-core {
  stop-color: var(--color-star-view);
  stop-opacity: 0.45;
}

.nebula-edge {
  stop-color: var(--color-star-view);
  stop-opacity: 0;
}
</style>
