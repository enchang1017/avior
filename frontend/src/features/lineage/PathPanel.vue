<script setup lang="ts">
import { computed } from 'vue'

import ConfidenceBadge from './ConfidenceBadge.vue'
import NotVisible from './NotVisible.vue'
import { hopsOf, type Hop } from './route'
import { qualified } from './scene'
import type { LineagePath } from './types'

const props = defineProps<{
  path: LineagePath
  chosen: number
  from: string
  to: string
  /** The chosen route's hop the operator picked, lit on the chart. */
  hop?: number | null
}>()
const emit = defineEmits<{ choose: [index: number]; hop: [index: number]; open: [id: string] }>()

const names = computed(() => new Map((props.path.nodes ?? []).map((node) => [node.id, node])))
const nameOf = (id: string) => names.value.get(id)?.name ?? id
const titleOf = (id: string) => {
  const node = names.value.get(id)
  return node === undefined ? id : qualified(node)
}

/** An arrow the way the hop is walked: one pointing back was walked against its edge. */
const arrow = (hop: Hop) => (hop.against ? `←${hop.word}─` : `─${hop.word}→`)
const arrowTitle = (hop: Hop) =>
  hop.against ? `${hop.word}, walked against the edge's direction` : hop.word
const routes = computed(() => props.path.routes ?? [])

function confidenceOf(index: number): 'exact' | 'parsed' {
  const edges = routes.value[index]?.edges ?? []
  return edges.some((edge) => edge.confidence === 'parsed') ? 'parsed' : 'exact'
}

const chosenHops = computed(() => {
  const route = routes.value[props.chosen]
  return route === undefined ? [] : hopsOf(route)
})

/** The chosen route in one sentence: what a change at its far end reaches, and how surely. */
const summary = computed(() => {
  const route = routes.value[props.chosen]
  const hops = chosenHops.value
  if (route === undefined || hops.length === 0) return ''
  const tally = new Map<string, number>()
  for (const hop of hops) tally.set(hop.word, (tally.get(hop.word) ?? 0) + 1)
  const through = [...tally].map(([word, count]) => `${count} ${word}`).join(', ')
  const sure = confidenceOf(props.chosen) === 'exact' ? 'all EXACT' : 'at best PARSED'
  const first = nameOf(route.nodes[0] ?? '')
  const last = nameOf(route.nodes.at(-1) ?? '')
  const count = `${hops.length} ${hops.length === 1 ? 'hop' : 'hops'}`
  return `Changing ${last}'s columns affects ${first}, through ${count} (${through}), ${sure}.`
})

/** What a hop rests on: a key's column pairs, a statement and its line, a definition, or a name. */
function evidenceOf(hop: Hop): string {
  const edge = hop.edge
  if (edge === null) return ''
  if (edge.kind === 'foreign_key') {
    const to = edge.toColumns ?? []
    return (edge.fromColumns ?? [])
      .map(
        (column, index) =>
          `${nameOf(edge.from)}.${column.name} → ${nameOf(edge.to)}.${to[index]?.name ?? '?'}`,
      )
      .join(', ')
  }
  const found = edge.evidence
  if (found === undefined) return ''
  const line = found.line === undefined ? '' : ` · line ${found.line}`
  return found.via ?? `${found.excerpt ?? ''}${line}`
}
</script>

<template>
  <section class="path-panel flex flex-col" data-testid="path-panel" aria-label="Routes">
    <header class="flex flex-col gap-1 border-b border-line px-3 py-3">
      <p class="m-0 eyebrow">Routes</p>
      <h2 class="m-0 font-mono text-md font-normal break-all text-target">{{ from }} → {{ to }}</h2>
      <p class="m-0 text-xs text-fg-soft">
        The shortest ways data can travel between them, five hops at most.
      </p>
    </header>
    <p v-if="routes.length === 0" class="no-route m-0 px-3 py-3 text-sm text-fg-soft" role="status">
      No route joins them within the hops asked for.
    </p>
    <ol class="m-0 list-none p-0">
      <li v-for="(route, index) in routes" :key="index">
        <button
          type="button"
          class="route flex w-full cursor-pointer items-start gap-2 border-0 border-b border-line bg-transparent px-3 py-2 text-left font-mono text-xs ease-colours hover:bg-surface-sunken"
          :class="index === chosen ? 'chosen text-target' : 'text-fg-soft'"
          :aria-pressed="index === chosen"
          @click="emit('choose', index)"
        >
          <span class="w-4 flex-none">{{ route.edges.length }}</span>
          <span class="chain flex min-w-0 flex-1 flex-wrap items-center gap-x-1 gap-y-0.5">
            <span class="hop-node" :title="titleOf(route.nodes[0] ?? '')">{{
              nameOf(route.nodes[0] ?? '')
            }}</span>
            <template v-for="(leg, step) in hopsOf(route)" :key="step">
              <span
                class="hop-edge text-2xs whitespace-nowrap text-fg-muted"
                :data-against="leg.against || undefined"
                :title="arrowTitle(leg)"
                >{{ arrow(leg) }}</span
              >
              <span class="hop-node" :title="titleOf(leg.to)">{{ nameOf(leg.to) }}</span>
            </template>
          </span>
          <ConfidenceBadge :confidence="confidenceOf(index)" />
        </button>
      </li>
    </ol>
    <section
      v-if="chosenHops.length > 0"
      class="route-detail flex flex-col border-b border-line"
      data-testid="route-detail"
      aria-label="The chosen route"
    >
      <p class="summary m-0 px-3 py-2 text-sm text-fg">{{ summary }}</p>
      <ol class="m-0 list-none p-0">
        <li v-for="(each, index) in chosenHops" :key="index">
          <div
            class="hop flex cursor-pointer flex-col gap-1 border-0 border-t border-line px-3 py-2 ease-colours hover:bg-surface-sunken"
            :class="index === hop ? 'picked bg-surface-sunken' : ''"
            role="button"
            tabindex="0"
            :aria-pressed="index === hop"
            @click="emit('hop', index)"
            @keydown.enter="emit('hop', index)"
          >
            <span class="flex flex-wrap items-center gap-x-1.5 font-mono text-xs">
              <button
                type="button"
                class="hop-name cursor-pointer border-0 bg-transparent p-0 font-mono text-xs text-fg hover:text-primary"
                :title="titleOf(each.from)"
                @click.stop="emit('open', each.from)"
              >
                {{ nameOf(each.from) }}
              </button>
              <span
                class="hop-edge text-2xs text-fg-muted"
                :data-against="each.against || undefined"
                :title="arrowTitle(each)"
                >{{ arrow(each) }}</span
              >
              <button
                type="button"
                class="hop-name cursor-pointer border-0 bg-transparent p-0 font-mono text-xs text-fg hover:text-primary"
                :title="titleOf(each.to)"
                @click.stop="emit('open', each.to)"
              >
                {{ nameOf(each.to) }}
              </button>
              <ConfidenceBadge
                v-if="each.edge"
                class="ml-auto"
                :confidence="each.edge.confidence"
              />
            </span>
            <span
              v-if="evidenceOf(each)"
              class="evidence truncate font-mono text-2xs text-fg-muted"
              :title="evidenceOf(each)"
              >{{ evidenceOf(each) }}</span
            >
          </div>
        </li>
      </ol>
    </section>
    <NotVisible />
  </section>
</template>
