<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'

import VIcon from '../../Media/VIcon/VIcon.vue'
import { edgePath, haloPaths, magnitude, strokeWeight } from './geometry'
import { placeLabels, type LabelBlock, type LabelMetrics } from './labels'
import type { Particle, particlesFor } from './particles'
import {
  sceneHeight,
  sceneWidth,
  stretch,
  type ChartEdge,
  type ChartScene,
  type ChartStar,
} from './types'

const props = defineProps<{
  scene: ChartScene
  summary: string
  /** Changes whenever the route being traced changes, so the trace plays once per choice. */
  traceKey: string
  /** The edge of a route's hop the operator picked, drawn heavy with both its ends lit. */
  selected?: string | null
}>()

const emit = defineEmits<{ choose: [id: string]; expand: [id: string] }>()

/** Something the page adds to the end of the legend's row, such as the dynamic SQL chip. */
defineSlots<{ legend?: () => unknown }>()

const viewBox = `${-sceneWidth / 2} ${-sceneHeight / 2} ${sceneWidth} ${sceneHeight}`

const svg = ref<SVGSVGElement | null>(null)
const probe = ref<SVGTextElement | null>(null)
const shownWidth = ref(sceneWidth)
const scale = computed(() => shownWidth.value / sceneWidth)
const typeSize = computed(() => ({ fontSize: `calc(var(--text-2xs) / ${scale.value})` }))

/** A character's width and a line's height in scene units, read off the drawn type. */
const glyph = ref<{ width: number; height: number } | null>(null)
const probeText = 'MMMMMMMMMM'
function measureType(): void {
  const text = probe.value
  if (text === null || typeof text.getComputedTextLength !== 'function') return
  const width = text.getComputedTextLength() / probeText.length
  const height = text.getBBox().height
  if (width > 0 && height > 0) glyph.value = { width, height }
}

let watching: ResizeObserver | null = null
onMounted(() => {
  measureType()
  // The monospace face is a web font, requested only once text is drawn in it;
  // a measure taken before it arrives is of the fallback, and would cut labels
  // to the wrong width. `ready` alone can resolve before the request is made.
  void document.fonts?.ready.then(measureType)
  document.fonts?.addEventListener('loadingdone', measureType)
  const element = svg.value
  if (element === null || typeof ResizeObserver === 'undefined') return
  watching = new ResizeObserver(([entry]) => {
    const width = entry?.contentRect.width ?? 0
    if (width > 0) shownWidth.value = width
    // After the type is redrawn at the new scale, or the measure is of the old one.
    void nextTick(measureType)
  })
  watching.observe(element)
})
onBeforeUnmount(() => {
  watching?.disconnect()
  document.fonts?.removeEventListener('loadingdone', measureType)
})

const stars = computed(() => [...props.scene.stars].sort((a, b) => (a.id < b.id ? -1 : 1)))
const byId = computed(() => new Map(props.scene.stars.map((star) => [star.id, star])))
const edgesKey = computed(
  () => `${props.scene.ring?.x ?? ''}:${props.scene.ring?.y ?? ''}:${props.traceKey}`,
)

const pointed = ref<string | null>(null)
const hoveredEdge = ref<ChartEdge | null>(null)
const pointedStar = computed(() => (pointed.value === null ? null : byId.value.get(pointed.value)))

function idFrom(event: Event): string | null {
  return (event.target as Element | null)?.getAttribute('data-id') ?? null
}

function click(event: Event): void {
  const star = byId.value.get(idFrom(event) ?? '')
  if (star === undefined) return
  if (star.hidden > 0) emit('expand', star.id)
  else emit('choose', star.id)
}

function over(event: Event): void {
  const target = event.target as Element | null
  const edge = target?.getAttribute('data-edge-key')
  if (edge !== null && edge !== undefined) {
    hoveredEdge.value = props.scene.edges.find((line) => line.key === edge) ?? null
    return
  }
  const id = idFrom(event)
  pointed.value = id !== null && byId.value.has(id) ? id : null
}

function out(event: Event): void {
  const target = event.target as Element | null
  if (target?.hasAttribute('data-edge-key') === true) hoveredEdge.value = null
  if (target?.hasAttribute('data-id') === true) pointed.value = null
}

function leave(): void {
  pointed.value = null
  hoveredEdge.value = null
}

const lit = (edge: ChartEdge) =>
  pointed.value !== null && (edge.from === pointed.value || edge.to === pointed.value)

const stroke: Record<ChartEdge['tone'], string> = {
  read: 'stroke-lineage-read',
  write: 'stroke-lineage-write',
  reference: 'stroke-chart-edge-near',
}

const amber = (edge: ChartEdge) => lit(edge) || edge.step !== null || edge.highlighted

/** A route's edges stay straight, a flight line; every other edge is an arc. */
const straight = (edge: ChartEdge) => edge.step !== null
/** Every edge's path, worked out once per scene: the edge, its halo and its emphasis share it. */
const paths = computed(
  () => new Map(props.scene.edges.map((edge) => [edge.key, edgePath(edge, straight(edge))])),
)
const pathOf = (edge: ChartEdge) => paths.value.get(edge.key) ?? ''
/** Halos near and far apart, so a far edge's halo is as dim as the edge. */
const halos = computed(() => [
  {
    far: false,
    paths: haloPaths(
      props.scene.edges.filter((edge) => !edge.far),
      pathOf,
    ),
  },
  {
    far: true,
    paths: haloPaths(
      props.scene.edges.filter((edge) => edge.far),
      pathOf,
    ),
  },
])
const tones = ['read', 'write', 'reference'] as const

const selectedEdge = computed(
  () => props.scene.edges.find((edge) => edge.key === props.selected) ?? null,
)
/** The hovered edge and the picked hop's, drawn heavy over the rest. */
const emphasised = computed(() => {
  const out: ChartEdge[] = []
  for (const edge of [hoveredEdge.value, selectedEdge.value])
    if (edge !== null && !out.includes(edge)) out.push(edge)
  return out
})
const litEnds = computed(() => {
  const edge = selectedEdge.value
  return new Set(edge === null ? [] : [edge.from, edge.to])
})

function edgeClass(edge: ChartEdge): string[] {
  return [
    amber(edge) ? 'stroke-target' : stroke[edge.tone],
    edge.far && !lit(edge) ? 'far' : '',
    edge.step !== null ? 'traced' : '',
  ]
}

function arrow(edge: ChartEdge): string {
  return `url(#lineage-arrow-${amber(edge) ? 'lit' : edge.tone})`
}

const place = (star: ChartStar) => ({ transform: `translate(${star.x}px, ${star.y}px)` })

/** Colour temperature by kind: a table warm white, a view blue, a routine amber. */
const temperature: Record<ChartStar['shape'], 'table' | 'view' | 'routine'> = {
  table: 'table',
  sequence: 'table',
  view: 'view',
  matview: 'view',
  function: 'routine',
  procedure: 'routine',
}
const inks = {
  table: ['stroke-star-table', 'fill-star-table'],
  view: ['stroke-star-view', 'fill-star-view'],
  routine: ['stroke-star-routine', 'fill-star-routine'],
} as const

function starClass(star: ChartStar): string[] {
  const hollow = star.shape === 'view' || star.shape === 'matview'
  const [stroke, fill] =
    star.tier === 'focus' || litEnds.value.has(star.id)
      ? ['stroke-target', 'fill-target']
      : inks[temperature[star.shape]]
  return [
    'star',
    stroke,
    hollow ? 'fill-surface' : fill,
    star.tier === 'far' ? 'far' : '',
    star.extension ? 'extension' : '',
    star.hidden > 0 ? 'hub' : '',
    litEnds.value.has(star.id) ? 'lit' : '',
  ]
}

/** A star's magnitude: larger and brighter the more it is connected, within bounds. */
const size = (star: ChartStar) =>
  (star.tier === 'focus' ? 7 : star.tier === 'near' ? 5 : 3.5) * magnitude(star.degree)
const brightness = (star: ChartStar) =>
  star.tier === 'focus' ? 1 : Math.min(1, 0.55 + star.degree * 0.075)

function diamond(star: ChartStar): string {
  const r = size(star) + 1
  return `M0 ${-r} L${r} 0 L0 ${r} L${-r} 0 Z`
}

function triangle(star: ChartStar): string {
  const r = size(star)
  return `M0 ${-r} L${r} ${r} L${-r} ${r} Z`
}

function labelOf(star: ChartStar): string {
  return star.hidden > 0 ? `${star.label} +${star.hidden}` : star.label
}

const badgeWords = { dynamic: 'DYNAMIC', may_reach_unknown: 'MAY REACH UNKNOWN' } as const

/** The target ring is drawn in screen pixels, so its radius in scene units follows the scale. */
const ringPixels = 22
const metrics = computed<LabelMetrics>(() => {
  const fallback = 6.6 / scale.value
  const width = glyph.value?.width ?? fallback
  const height = glyph.value?.height ?? width * 2
  return {
    charWidth: width,
    lineHeight: height,
    ringRadius: ringPixels / scale.value,
    gap: 10,
    pad: 4,
  }
})

const labels = computed(() =>
  placeLabels(
    stars.value.map((star) => ({
      id: star.id,
      x: star.x,
      y: star.y,
      tier: star.tier,
      labelled: star.labelled,
      text: labelOf(star),
      title: star.title,
      badge: star.badge === null ? null : badgeWords[star.badge],
    })),
    metrics.value,
  ),
)

const labelFill = (label: LabelBlock) =>
  label.tier === 'focus'
    ? 'fill-target'
    : label.tier === 'near'
      ? 'fill-fg-soft'
      : 'far fill-fg-muted'

const labelAt = (label: LabelBlock) => ({ transform: `translate(${label.x}px, ${label.y}px)` })

/** A halo in the ground colour, so no edge is ever drawn across a label. */
const halo = computed(() => ({ strokeWidth: `${metrics.value.charWidth / 2}px` }))

/*
 * Particles only ever show on a hovered edge or a chosen route, so their code
 * is fetched on the first pointer over the chart or the first route chosen,
 * and never by a drawing nobody touches.
 */
const flow = shallowRef<typeof particlesFor | null>(null)
function wantFlow(): void {
  if (flow.value === null)
    void import('./particles').then((module) => (flow.value = module.particlesFor))
}
watch(
  () => props.scene.edges.some((edge) => edge.step !== null),
  (routed) => {
    if (routed) wantFlow()
  },
  { immediate: true },
)
const particles = computed<Particle[]>(() =>
  flow.value === null ? [] : flow.value(props.scene.edges, hoveredEdge.value, scale.value),
)
const particleStyle = (particle: Particle) => ({
  '--flow-from': particle.from,
  '--flow-to': particle.to,
  '--flow-offset': particle.offset,
})

const across = (point: { x: number; y: number }) => ({
  left: `${((point.x + sceneWidth / 2) / sceneWidth) * 100}%`,
  top: `${((point.y + sceneHeight / 2) / sceneHeight) * 100}%`,
})

const ringStyle = computed(() => (props.scene.ring === null ? null : across(props.scene.ring)))

const hoverLabel = computed(() => {
  const edge = hoveredEdge.value
  if (edge !== null) {
    return { x: (edge.x1 + edge.x2) / 2 + 6, y: (edge.y1 + edge.y2) / 2 - 6, text: edge.caption }
  }
  const star = pointedStar.value
  if (star === undefined || star === null || star.labelled) return null
  const badge = star.badge === null ? '' : ` · ${badgeWords[star.badge]}`
  return { x: star.x + 8, y: star.y - 6, text: `${star.title}${badge}` }
})

const markerTones = ['read', 'write', 'reference', 'lit'] as const
const markerClass: Record<(typeof markerTones)[number], string> = {
  read: 'fill-lineage-read',
  write: 'fill-lineage-write',
  reference: 'fill-chart-edge-near',
  lit: 'fill-target',
}
</script>

<template>
  <figure class="m-0 flex min-h-0 min-w-0 flex-col">
    <div class="relative">
      <svg
        ref="svg"
        role="img"
        :aria-label="summary"
        :viewBox="viewBox"
        :style="typeSize"
        class="lineage-chart block h-auto w-full font-mono select-none"
        data-testid="lineage-chart"
        @click="click"
        @pointerenter="wantFlow"
        @pointerover="over"
        @pointerout="out"
        @pointerleave="leave"
      >
        <defs>
          <marker
            v-for="tone in markerTones"
            :id="`lineage-arrow-${tone}`"
            :key="tone"
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            markerUnits="userSpaceOnUse"
            orient="auto"
          >
            <path d="M0 1 L9 5 L0 9 Z" :class="markerClass[tone]" />
          </marker>
          <marker
            id="lineage-mark-foreign_key"
            viewBox="0 0 24 24"
            refX="12"
            refY="12"
            markerWidth="12"
            markerHeight="12"
            markerUnits="userSpaceOnUse"
            orient="0"
          >
            <circle cx="12" cy="12" r="12" class="fill-surface" />
            <g transform="translate(4 4)" class="text-fg-soft">
              <VIcon name="key" />
            </g>
          </marker>
          <marker
            id="lineage-mark-trigger"
            viewBox="0 0 24 24"
            refX="12"
            refY="12"
            markerWidth="12"
            markerHeight="12"
            markerUnits="userSpaceOnUse"
            orient="0"
          >
            <circle cx="12" cy="12" r="12" class="fill-surface" />
            <g transform="translate(4 4)" class="text-fg-soft">
              <VIcon name="zap" />
            </g>
          </marker>
        </defs>

        <g class="chart-enter fill-none stroke-line" aria-hidden="true">
          <ellipse
            v-for="ring in scene.rings"
            :key="ring"
            cx="0"
            cy="0"
            :rx="ring * stretch"
            :ry="ring"
            stroke-dasharray="1 4"
          />
        </g>

        <g class="halos fill-none" aria-hidden="true">
          <template v-for="set in halos" :key="String(set.far)">
            <path
              v-for="tone in tones"
              :key="tone"
              :d="set.paths[tone]"
              :class="stroke[tone]"
              stroke-width="6"
              :stroke-opacity="set.far ? 0.05 : 0.15"
              stroke-linecap="round"
              vector-effect="non-scaling-stroke"
            />
          </template>
        </g>

        <g :key="edgesKey" class="edges fill-none" aria-hidden="true">
          <path
            v-for="edge in scene.edges"
            :key="edge.key"
            data-edge
            :data-edge-key="edge.key"
            :data-tone="edge.tone"
            :data-confidence="edge.parsed ? 'parsed' : 'exact'"
            :data-mark="edge.mark ?? undefined"
            :data-lit="lit(edge) ? 'true' : undefined"
            :data-step="edge.step ?? undefined"
            :data-highlighted="edge.highlighted ? 'true' : undefined"
            :data-selected="edge.key === selected ? 'true' : undefined"
            :d="pathOf(edge)"
            :class="edgeClass(edge)"
            :style="edge.step === null ? undefined : { '--step': edge.step }"
            :stroke-dasharray="edge.parsed ? '5 4' : 'none'"
            :marker-mid="edge.mark === null ? 'none' : `url(#lineage-mark-${edge.mark})`"
            :marker-end="arrow(edge)"
            :stroke-width="strokeWeight(edge)"
            vector-effect="non-scaling-stroke"
            class="edge"
          />
        </g>

        <g class="emphasis pointer-events-none fill-none" aria-hidden="true">
          <template v-for="edge in emphasised" :key="edge.key">
            <path
              :d="pathOf(edge)"
              :class="stroke[edge.tone]"
              stroke-width="6"
              stroke-opacity="0.45"
              stroke-linecap="round"
              vector-effect="non-scaling-stroke"
            />
            <path
              :d="pathOf(edge)"
              :class="amber(edge) ? 'stroke-target' : stroke[edge.tone]"
              :data-emphasis="edge.key"
              stroke-width="3"
              vector-effect="non-scaling-stroke"
            />
          </template>
        </g>

        <g class="chart-enter cursor-pointer">
          <template v-for="star in stars" :key="star.id">
            <path
              v-if="star.shape === 'function' || star.shape === 'sequence'"
              :d="star.shape === 'function' ? diamond(star) : triangle(star)"
              :data-id="star.id"
              data-node
              :data-tier="star.tier"
              :data-shape="star.shape"
              :data-hidden="star.hidden || undefined"
              :class="starClass(star)"
              :style="place(star)"
              :fill-opacity="brightness(star)"
              stroke-width="1.5"
            />
            <rect
              v-else-if="star.shape === 'procedure'"
              :x="-size(star)"
              :y="-size(star)"
              :width="size(star) * 2"
              :height="size(star) * 2"
              :data-id="star.id"
              data-node
              :data-tier="star.tier"
              :data-shape="star.shape"
              :data-hidden="star.hidden || undefined"
              :class="starClass(star)"
              :style="place(star)"
              :fill-opacity="brightness(star)"
              stroke-width="1.5"
            />
            <circle
              v-else
              cx="0"
              cy="0"
              :r="star.hidden > 0 ? size(star) + 3 : size(star)"
              :data-id="star.id"
              data-node
              :data-tier="star.tier"
              :data-shape="star.shape"
              :data-hidden="star.hidden || undefined"
              :class="starClass(star)"
              :style="place(star)"
              :fill-opacity="brightness(star)"
              :stroke-width="star.shape === 'matview' ? 3 : 1.5"
              :stroke-dasharray="star.hidden > 0 ? '2 2' : 'none'"
            />
          </template>
        </g>

        <g class="chart-enter pointer-events-none" aria-hidden="true">
          <text
            v-for="label in labels"
            :key="label.id"
            x="0"
            y="0"
            :text-anchor="label.anchor"
            class="label halo stroke-ground"
            :class="labelFill(label)"
            :data-label-for="label.id"
            :style="[labelAt(label), halo]"
          >
            <title v-if="label.lines[0] !== label.full">{{ label.full }}</title>
            <tspan
              v-for="(line, index) in label.lines"
              :key="index"
              x="0"
              :dy="index === 0 ? 0 : metrics.lineHeight"
              :class="index === label.badgeLine ? 'star-badge fill-warn' : undefined"
              :data-badge="index === label.badgeLine ? byId.get(label.id)?.badge : undefined"
              :data-for="index === label.badgeLine ? label.id : undefined"
            >
              {{ line }}
            </tspan>
          </text>
          <text ref="probe" class="probe" visibility="hidden" x="0" y="0">{{ probeText }}</text>
          <text
            v-if="hoverLabel"
            :x="hoverLabel.x"
            :y="hoverLabel.y"
            class="hover-label fill-fg"
            data-testid="lineage-hover-label"
          >
            {{ hoverLabel.text }}
          </text>
        </g>
      </svg>

      <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
        <span
          v-if="ringStyle"
          :key="`${ringStyle.left}${ringStyle.top}`"
          class="target-ring absolute -mt-4 -ml-4 size-8 rounded-pill border border-target"
          :style="ringStyle"
        />
        <span
          v-for="particle in particles"
          :key="particle.key"
          class="particle absolute top-0 left-0 -mt-0.5 -ml-0.5 size-1 rounded-pill bg-target"
          data-particle
          :style="particleStyle(particle)"
        />
      </div>
    </div>

    <div class="flex items-start gap-4 border-t border-line px-3 py-2">
      <ul
        class="m-0 flex min-w-0 flex-1 list-none flex-wrap items-center gap-x-4 gap-y-1 p-0 font-mono text-2xs text-fg-muted"
        aria-label="Legend"
      >
        <li class="flex items-center gap-1.5">
          <span class="size-2 rounded-pill bg-fg-soft" />table
        </li>
        <li class="flex items-center gap-1.5">
          <span class="size-2 rounded-pill border border-fg-soft" />view
        </li>
        <li class="flex items-center gap-1.5">
          <span class="size-2 rounded-pill border-2 border-fg-soft" />mat. view
        </li>
        <li class="flex items-center gap-1.5">
          <span class="size-2 rotate-45 bg-fg-soft" />function
        </li>
        <li class="flex items-center gap-1.5"><span class="size-2 bg-fg-soft" />procedure</li>
        <li class="flex items-center gap-1.5"><span class="h-0.5 w-4 bg-lineage-read" />read</li>
        <li class="flex items-center gap-1.5"><span class="h-0.5 w-4 bg-lineage-write" />write</li>
        <li class="flex items-center gap-1.5">
          <span class="h-0.5 w-4 bg-chart-edge-near" />reference
        </li>
        <li class="flex items-center gap-1.5">
          <span class="w-4 border-t border-dashed border-fg-soft" />PARSED
        </li>
        <li class="flex items-center gap-1.5"><VIcon name="key" size="xs" />foreign key</li>
        <li class="flex items-center gap-1.5"><VIcon name="zap" size="xs" />trigger</li>
      </ul>
      <div v-if="$slots.legend" class="flex flex-none items-center"><slot name="legend" /></div>
    </div>
  </figure>
</template>

<style scoped>
.chart-enter {
  animation: var(--animate-chart-in);
}

.edges {
  animation: var(--animate-chart-fade);
}

.star,
.label {
  transition: transform var(--duration-glide) var(--ease-out);
}

.star.far,
.label.far,
.edge.far {
  opacity: 0.35;
}

.edge {
  pointer-events: visibleStroke;
}

.traced {
  animation: var(--animate-path-trace);
  animation-delay: calc(var(--stagger-trace) * var(--step));
}

.halo {
  paint-order: stroke;
  stroke-linejoin: round;
}

.emphasis {
  animation: var(--animate-fade);
}

.target-ring {
  opacity: 0;
  animation: var(--animate-target-ring);
}

.particle {
  opacity: 0;
  will-change: transform;
  animation: var(--animate-flow-particle);
  animation-delay: calc(var(--duration-flow) * var(--flow-offset));
}
</style>
