<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { adjacency, drawnPart, type NodeRef, type SchemaGraph } from './graph'
import { chartHeight, chartWidth, radialLayout, stretch, type Star } from './layout'
import { chartCaption, describeChart, rowsLabel, showingNote } from './summary'

const props = defineProps<{
  graph: SchemaGraph
  database: string
  target: NodeRef | null
}>()

const emit = defineEmits<{ choose: [ref: NodeRef & { kind: Star['kind'] }] }>()

const drawn = computed(() => drawnPart(props.graph, props.target))
const layout = computed(() => {
  const start = performance.now()
  const placed = radialLayout(drawn.value.nodes, drawn.value.edges, props.target)
  performance.measure('schema-chart:layout', { start })
  return placed
})
const byKey = computed(() => new Map(layout.value.stars.map((star) => [star.key, star])))
const centre = computed(() => layout.value.stars.find((star) => star.ring === 0) ?? null)
const summary = computed(() =>
  describeChart(props.database, drawn.value.nodes.length, drawn.value.edges, props.target),
)
const note = computed(() => showingNote(drawn.value.nodes.length, drawn.value.total))
const caption = computed(() => chartCaption(props.database, drawn.value.nodes.length, note.value))
const viewBox = `${-chartWidth / 2} ${-chartHeight / 2} ${chartWidth} ${chartHeight}`

/*
 * The chart is drawn in a 400-unit box and shown at the pane's width, so a
 * font size set in the box shrinks with it: text-3xs came out near 6 px. The
 * type is set in the box at the size that shows as text-3xs on screen.
 */
const svg = ref<SVGSVGElement | null>(null)
const shownWidth = ref(chartWidth)
const typeScale = computed(() => chartWidth / shownWidth.value)
const typeSize = computed(() => ({ fontSize: `calc(var(--text-3xs) * ${typeScale.value})` }))
// The centre's second line sits one line below its name, at whatever size the type is shown.
const metaDrop = computed(() => 12 * typeScale.value)
let watching: ResizeObserver | null = null
onMounted(() => {
  const element = svg.value
  if (element === null || typeof ResizeObserver === 'undefined') return
  watching = new ResizeObserver(([entry]) => {
    const width = entry?.contentRect.width ?? 0
    if (width > 0) shownWidth.value = width
  })
  watching.observe(element)
})
onBeforeUnmount(() => watching?.disconnect())

const hovered = ref<Star | null>(null)
const pointed = ref<string | null>(null)
const around = computed(() => adjacency(drawn.value.edges))
const lit = computed(() => {
  const key = pointed.value
  return key === null ? null : new Set([key, ...(around.value.get(key) ?? [])])
})
const rings = computed(() =>
  ([0, 1, 2] as const).map((ring) => layout.value.stars.filter((star) => star.ring === ring)),
)
const edgesKey = computed(() => centre.value?.key ?? '')

const place = (star: Star) => ({ transform: `translate(${star.x}px, ${star.y}px)` })

function starFrom(event: Event): Star | null {
  const key = (event.target as Element | null)?.getAttribute('data-key')
  return key === null || key === undefined ? null : (byKey.value.get(key) ?? null)
}

function choose(event: Event): void {
  const star = starFrom(event)
  if (star !== null) emit('choose', { schema: star.schema, name: star.name, kind: star.kind })
}

function hover(event: Event): void {
  const star = starFrom(event)
  hovered.value = star !== null && !star.labelled ? star : null
  pointed.value = star?.key ?? null
}

function leave(): void {
  hovered.value = null
  pointed.value = null
}

function lineLit(line: { from: string; to: string }): boolean {
  return pointed.value !== null && (line.from === pointed.value || line.to === pointed.value)
}

function meta(star: Star): string {
  const rows = rowsLabel(star.approxRows)
  const columns = `${star.columns} col`
  return star.kind === 'view' ? `view · ${columns}` : [rows, columns].filter(Boolean).join(' · ')
}

const radius = (star: Star) => (star.ring === 0 ? 5 : star.ring === 1 ? 3.5 : 2.5)

function starClass(star: Star): string {
  if (star.ring === 0) return 'fill-target stroke-target'
  if (star.kind === 'view')
    return star.ring === 1 ? 'fill-surface stroke-fg' : 'fill-surface stroke-fg-muted'
  return star.ring === 1 ? 'fill-fg stroke-fg' : 'fill-fg-muted stroke-fg-muted'
}
</script>

<template>
  <figure class="m-0 flex min-w-0 flex-col gap-1">
    <div class="relative">
      <svg
        ref="svg"
        role="img"
        :aria-label="summary"
        :viewBox="viewBox"
        :style="typeSize"
        class="schema-chart block h-auto w-full font-mono select-none"
        data-testid="schema-chart"
      >
        <g class="chart-enter fill-none stroke-line" aria-hidden="true">
          <ellipse
            v-for="ring in layout.rings"
            :key="ring"
            cx="0"
            cy="0"
            :rx="ring * stretch"
            :ry="ring"
            stroke-dasharray="1 3"
          />
        </g>
        <g :key="edgesKey" class="edges fill-none" :class="lit ? 'is-lit' : ''" aria-hidden="true">
          <line
            v-for="line in layout.lines"
            :key="line.key"
            data-edge
            :x1="line.x1"
            :y1="line.y1"
            :x2="line.x2"
            :y2="line.y2"
            vector-effect="non-scaling-stroke"
            :class="[
              line.near ? 'stroke-chart-edge-near' : 'stroke-chart-edge',
              lineLit(line) ? 'lit' : '',
            ]"
            :stroke-dasharray="line.kind === 'view_dependency' ? '3 2' : 'none'"
          />
        </g>
        <circle
          v-if="centre"
          cx="0"
          cy="0"
          r="10"
          class="chart-enter fill-target-wash stroke-target-line"
          aria-hidden="true"
        />
        <g
          class="stars cursor-pointer"
          :class="lit ? 'is-lit' : ''"
          @click="choose"
          @pointerover="hover"
          @pointerleave="leave"
        >
          <g
            v-for="(members, ring) in rings"
            :key="ring"
            class="chart-enter"
            :class="`ring-${ring}`"
          >
            <g
              v-for="star in members"
              :key="star.key"
              class="node"
              :class="lit?.has(star.key) ? 'lit' : ''"
              :style="place(star)"
            >
              <circle
                :data-key="star.key"
                data-node
                :data-ring="star.ring"
                :data-kind="star.kind"
                cx="0"
                cy="0"
                :r="radius(star)"
                :class="starClass(star)"
                class="star"
              />
              <text
                v-if="star.labelled"
                x="7"
                y="3"
                class="pointer-events-none"
                :class="star.ring === 0 ? 'fill-target' : 'fill-fg-soft'"
              >
                {{ star.name }}
              </text>
            </g>
          </g>
        </g>
        <g class="pointer-events-none" aria-hidden="true">
          <text v-if="centre" x="7" :y="3 + metaDrop" class="fill-fg-muted">
            {{ meta(centre) }}
          </text>
          <text v-if="hovered" :x="hovered.x + 6" :y="hovered.y - 5" class="fill-fg">
            {{ hovered.schema }}.{{ hovered.name }}
          </text>
        </g>
      </svg>
      <span
        v-if="centre"
        :key="centre.key"
        class="target-ring pointer-events-none absolute top-1/2 left-1/2 -mt-3 -ml-3 size-6 rounded-pill border border-target"
        aria-hidden="true"
      />
    </div>
    <!-- One line in a narrow pane: cut short with an ellipsis, whole on hover. -->
    <figcaption
      class="truncate px-1 font-mono text-3xs tracking-hud text-fg-muted uppercase"
      :title="caption"
    >
      {{ database }} · {{ drawn.nodes.length }} objects · fk/view edges<span
        v-if="note"
        class="showing text-target"
      >
        · {{ note }}</span
      >
    </figcaption>
  </figure>
</template>

<style scoped>
.chart-enter {
  animation: var(--animate-chart-in);
}

.ring-1 {
  animation-delay: 70ms;
}

.ring-2 {
  animation-delay: 140ms;
}

.edges {
  animation: var(--animate-chart-fade);
}

.node {
  transition: transform var(--duration-glide) var(--ease-out);
}

.stars.is-lit .node:not(.lit),
.edges.is-lit line:not(.lit) {
  opacity: 0.25;
}

.target-ring {
  opacity: 0;
  animation: var(--animate-target-ring);
}
</style>
