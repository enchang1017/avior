<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { VBanner, VButton, VIcon } from '@/components'
import VLineageChart from '@/components/Data/VLineageChart/VLineageChart.vue'
import { api, classify, describeFailure, unreachable, type TableRef } from '@/shared/api'
import DelayedLoader from '@/shared/DelayedLoader.vue'
import { lazyPart } from './lazyPart'
import { fullStamp, stamp } from '@/shared/timestamp'
import { useTargetStore } from '@/stores/target'
import { lineageRoute, readAddress, writeAddress, type Address, type Hops } from './address'
import DynamicTray from './DynamicTray.vue'
import { names, refKey, refLabel, refOf } from './focus'
import { focusText, type FocusPick } from './focusText'
import { radialLayout } from './layout'
import type { pathLayout } from './pathLayout'
import LineageToolbar from './LineageToolbar.vue'
import { buildScene, describeNeighbourhood, dynamicRoutines, edgeKey, qualified } from './scene'
import {
  confidences,
  isRoutine,
  type FocusRef,
  nodeKinds,
  type LineageConfidence,
  type LineageNode,
  type LineageNodeKind,
} from './types'
import { useLineageGraph, useLineagePath } from './useLineage'

// No panel is needed to draw a neighbourhood, so none is fetched before one
// is drawn: the chart paints first and the panel beside it a moment later.
// An empty page is not drawn on the way to a neighbourhood, so it is not part
// of the first load (ADR 0009 P3).
const VEmpty = defineAsyncComponent(() => import('@/components/Feedback/VEmpty/VEmpty.vue'))
const ColumnPanel = lazyPart(() => import('./ColumnPanel.vue'), 'Loading the columns')
const PathPanel = lazyPart(() => import('./PathPanel.vue'), 'Loading the routes')
const RoutinePanel = lazyPart(() => import('./RoutinePanel.vue'), 'Loading the routine')
// The overview is fetched when the operator reaches for the Overview button, so
// its first drawing is not also its first compile.
const loadOverview = () => import('./LineageOverview.vue')
const LineageOverview = lazyPart(loadOverview, 'Loading the overview')

const instanceId = '00000000-0000-0000-0000-0000000000a1'

const route = useRoute()
const router = useRouter()
const address = computed(() => readAddress(route.query))

function go(patch: Partial<Address>): void {
  void router.replace({ name: lineageRoute, query: writeAddress({ ...address.value, ...patch }) })
}

// FEAT-065 AC-3: TARGET names what the operator is pointed at, and on this page
// that is the focus. Taken from the address, so it is right before the answer
// arrives and follows every change of focus.
const target = useTargetStore()
watch(
  () => [address.value.database, address.value.focus] as const,
  ([database, focus]) => {
    if (database !== null)
      target.aim(database, focus === null ? null : `${focus.schema}.${focus.name}`)
  },
  { immediate: true },
)

const databases = ref<string[]>([])
const tables = ref<TableRef[]>([])
const listFailure = ref('')
const databasesPending = ref(true)
const tablesPending = ref(false)

async function listed<T>(work: () => Promise<{ data?: T; error?: unknown; response: Response }>) {
  try {
    const { data, error, response } = await work()
    if (data === undefined) listFailure.value = describeFailure(classify(response.status, error))
    return data
  } catch (thrown) {
    listFailure.value = describeFailure(unreachable(thrown))
    return undefined
  }
}

onMounted(async () => {
  const found = await listed(() =>
    api.GET('/schema/databases', { params: { query: { instanceId } } }),
  )
  databases.value = found ?? []
  databasesPending.value = false
})

watch(
  () => address.value.database,
  async (database) => {
    tables.value = []
    tablesPending.value = database !== null
    if (database === null) return
    const found = await listed(() =>
      api.GET('/schema/tables', { params: { query: { instanceId, database } } }),
    )
    if (address.value.database !== database) return
    tables.value = found ?? []
    tablesPending.value = false
  },
  { immediate: true },
)

const kinds = ref(new Set<LineageNodeKind>(nodeKinds))
const confidence = ref(new Set<LineageConfidence>(confidences))
const extensions = ref(false)
const allowThree = ref(address.value.hops === 3)
const maxHops = ref(5)

function toggled<T>(set: Set<T>, value: T): Set<T> {
  const next = new Set(set)
  if (next.has(value)) {
    if (next.size > 1) next.delete(value)
  } else next.add(value)
  return next
}

const everything = <T,>(set: Set<T>, all: readonly T[]) =>
  set.size === all.length ? null : all.filter((value) => set.has(value))

const graph = useLineageGraph(() => {
  const { database, focus, mode, hops } = address.value
  if (database === null || focus === null || mode !== 'neighbourhood') return null
  return {
    instanceId,
    database,
    focus,
    hops: hops === 3 && !allowThree.value ? 2 : hops,
    kinds: everything(kinds.value, nodeKinds),
    confidence: everything(confidence.value, confidences),
    extensions: extensions.value,
  }
})

const path = useLineagePath(() => {
  const { database, focus, mode, to } = address.value
  if (database === null || focus === null || to === null || mode !== 'path') return null
  return { instanceId, database, from: focus, to, maxHops: maxHops.value }
})

const chosenRoute = ref(0)
const chosenHop = ref<number | null>(null)
const traced = ref(0)
watch(path.answer, () => {
  chosenRoute.value = 0
  chosenHop.value = null
  traced.value += 1
})

function chooseRoute(index: number): void {
  chosenRoute.value = index
  chosenHop.value = null
  traced.value += 1
}

/** The picked hop's edge, lit on the chart; a second pick of the same hop lets it go. */
function chooseHop(index: number): void {
  chosenHop.value = chosenHop.value === index ? null : index
}
const selectedEdge = computed(() => {
  const edge =
    chosenHop.value === null
      ? undefined
      : path.answer.value?.routes?.[chosenRoute.value]?.edges[chosenHop.value]
  return edge === undefined ? null : edgeKey(edge)
})

/** A name in a hop becomes the focus of a neighbourhood. */
function openFromRoute(id: string): void {
  const node = known.value.get(id)
  if (node !== undefined)
    go({ focus: refOf(node, known.value.values()), mode: 'neighbourhood', column: null })
}

const answered = computed(() => graph.graph.value)
const known = shallowRef(new Map<string, LineageNode>())
watch([answered, path.answer], ([neighbourhood, routes]) => {
  const next = new Map(known.value)
  for (const node of [
    ...(neighbourhood?.nodes ?? []),
    ...(neighbourhood?.dynamic ?? []),
    ...(routes?.nodes ?? []),
  ])
    next.set(node.id, node)
  known.value = next
})

const choices = computed(() => {
  const options = new Map<string, FocusRef>()
  const add = (ref: FocusRef) => {
    const key = refKey(ref)
    if (!options.has(key)) options.set(key, ref)
  }
  for (const table of tables.value)
    add({ schema: table.schema, name: table.name, kind: table.kind === 'view' ? 'view' : 'table' })
  const every = [...known.value.values()]
  for (const node of every) add(refOf(node, every))
  for (const ref of [address.value.focus, address.value.to]) if (ref !== null) add(ref)
  return options
})

const focusRefs = computed(() => [...choices.value.values()])
const focusValue = computed(() =>
  focusText(
    address.value.focus,
    address.value.mode === 'neighbourhood' ? address.value.column : null,
  ),
)

/** The columns of each relation the picker has been asked about, by its key. */
const pickerColumns = shallowRef(new Map<string, string[]>())
const columnsOf = (ref: FocusRef) => pickerColumns.value.get(refKey(ref))

async function needColumns(ref: FocusRef): Promise<void> {
  const database = address.value.database
  const key = refKey(ref)
  if (database === null || pickerColumns.value.has(key)) return
  const found = await listed(() =>
    api.GET('/schema/columns', {
      params: { query: { instanceId, database, schema: ref.schema, table: ref.name } },
    }),
  )
  if (found !== undefined && address.value.database === database)
    pickerColumns.value = new Map(pickerColumns.value).set(
      key,
      found.map((column) => column.name),
    )
}

watch(
  () => address.value.database,
  () => (pickerColumns.value = new Map()),
)

const focusNode = computed(() => {
  const focus = address.value.focus
  if (focus === null) return null
  const resolved = address.value.mode === 'neighbourhood' ? answered.value?.focus : undefined
  const byId = resolved === undefined ? undefined : known.value.get(resolved)
  return byId ?? [...known.value.values()].find((node) => names(focus, node)) ?? null
})

/** The overloads the page has seen for a routine the server could not settle by name. */
const overloads = computed(() => {
  if (active.value.refusal.value !== 'lineage.routine_ambiguous') return []
  const asked = [address.value.focus, address.value.to].filter((ref) => ref?.kind === 'routine')
  return [...known.value.values()].filter((node) =>
    asked.some(
      (ref) =>
        ref !== null &&
        isRoutine(node.kind) &&
        node.schema === ref.schema &&
        node.name === ref.name,
    ),
  )
})

function chooseOverload(node: LineageNode): void {
  const ref: FocusRef = { ...refOf(node), signature: node.arguments ?? '' }
  const toIt =
    address.value.to !== null &&
    address.value.to.name === node.name &&
    address.value.mode === 'path'
  go(toIt ? { to: ref } : { focus: ref, column: null })
}

// Only path mode lays routes out, so only path mode fetches how.
const pathLayoutOf = shallowRef<typeof pathLayout | null>(null)
watch(
  () => address.value.mode,
  (mode) => {
    if (mode === 'path' && pathLayoutOf.value === null)
      void import('./pathLayout').then((module) => (pathLayoutOf.value = module.pathLayout))
  },
  { immediate: true },
)

const scene = computed(() => {
  const start = performance.now()
  let drawn = null
  if (address.value.mode === 'path') {
    const pathLayout = pathLayoutOf.value
    const answer = path.answer.value
    const routes = answer?.routes ?? []
    const focus = routes[0]?.nodes[0]
    const to = routes[0]?.nodes.at(-1)
    if (pathLayout !== null && answer !== null && focus !== undefined && to !== undefined) {
      drawn = buildScene({
        nodes: answer.nodes ?? [],
        edges: routes.flatMap((one) => one.edges),
        placement: pathLayout(routes, focus, to),
        rings: [],
        focus,
        route: routes[chosenRoute.value]?.edges ?? [],
      })
    }
  } else {
    const answer = answered.value
    if (answer !== null) {
      const focus = answer.focus ?? null
      const edges = answer.edges ?? []
      const nodes = answer.nodes ?? []
      const radial = radialLayout(nodes, edges, focus)
      drawn = buildScene({
        nodes,
        edges,
        placement: radial.placement,
        rings: radial.rings,
        focus,
        column: address.value.column,
      })
    }
  }
  performance.measure('lineage:layout', { start })
  return drawn
})

function setMode(mode: Address['mode']): void {
  go(mode === 'overview' ? { mode, focus: null, to: null, column: null } : { mode })
}

/** The camera has landed: on the cluster's most connected object, or on the focus chosen. */
function landed(anchor: LineageNode | null): void {
  go(
    anchor === null
      ? { mode: 'neighbourhood' }
      : { mode: 'neighbourhood', focus: refOf(anchor, [anchor]), column: null },
  )
}

/** Zooming out past a threshold, with a pinch or ⌘ and the wheel, opens the overview. */
const zoomOutAt = 240
let zoomedOut = 0
function zoom(event: WheelEvent): void {
  if (address.value.mode !== 'neighbourhood' || !(event.ctrlKey || event.metaKey)) return
  event.preventDefault()
  zoomedOut = Math.max(0, zoomedOut + event.deltaY)
  if (zoomedOut > zoomOutAt) {
    zoomedOut = 0
    setMode('overview')
  }
}

const summary = computed(() => {
  const answer = answered.value
  if (address.value.mode === 'path') {
    const count = (path.answer.value?.routes ?? []).length
    return `${count} ${count === 1 ? 'route' : 'routes'} between the two objects.`
  }
  return answer === null ? '' : describeNeighbourhood(answer, address.value.hops)
})

const dynamic = computed(
  () => answered.value?.dynamic ?? dynamicRoutines(answered.value?.nodes ?? []),
)
const drawnIds = computed(() => new Set((scene.value?.stars ?? []).map((star) => star.id)))
const dynamicTotal = computed(() =>
  Math.max(answered.value?.snapshot?.dynamic ?? 0, dynamic.value.length),
)

const readout = computed(() => {
  const answer = answered.value
  if (address.value.mode === 'path' || answer === null) return ''
  const drawn = answer.nodes?.length ?? 0
  const total = Math.max(answer.total?.nodes ?? drawn, drawn)
  const hops = address.value.hops
  return `${drawn} of ${total} · ${hops} ${hops === 1 ? 'hop' : 'hops'}`
})

const snapshot = computed(() =>
  address.value.mode === 'path' ? path.answer.value?.snapshot : answered.value?.snapshot,
)

const active = computed(() => (address.value.mode === 'path' ? path : graph))
const failure = computed(() => listFailure.value || active.value.failure.value)
const building = computed(() => active.value.building.value && scene.value === null)

/** The canvas is asked about something and has had no answer to draw yet (FEAT-074 AC-4). */
const awaiting = computed(() => {
  const { database, focus, mode, to } = address.value
  if (database === null || focus === null || mode === 'overview') return false
  if (mode === 'path' && to === null) return false
  if (failure.value !== '' || active.value.building.value) return false
  const answer = mode === 'path' ? path.answer.value : answered.value
  return answer === null || (mode === 'path' && pathLayoutOf.value === null)
})

function focusOn(id: string): void {
  const node = known.value.get(id)
  if (node !== undefined) go({ focus: refOf(node, known.value.values()), column: null })
}

function pickFocus(pick: FocusPick | null): void {
  const column = address.value.mode === 'neighbourhood' ? (pick?.column ?? null) : null
  go({ focus: pick?.ref ?? null, column })
}

function setHops(hops: Hops): void {
  go({ hops })
}

function allowThreeHops(allowed: boolean): void {
  allowThree.value = allowed
}
</script>

<template>
  <div class="lineage-page list-page" data-testid="lineage-page">
    <LineageToolbar
      :databases="databases"
      :database="address.database"
      :focus-refs="focusRefs"
      :focus-value="focusValue"
      :to-value="focusText(address.to, null)"
      :columns-of="columnsOf"
      :mode="address.mode"
      :hops="address.hops"
      :allow-three="allowThree"
      :max-hops="maxHops"
      :kinds="kinds"
      :confidence="confidence"
      :extensions="extensions"
      :extensions-hidden="answered?.extensionsHidden ?? 0"
      :databases-pending="databasesPending"
      :objects-pending="tablesPending"
      @database="go({ database: $event, focus: null, to: null, column: null })"
      @pick-focus="pickFocus"
      @need-columns="needColumns"
      @mode="setMode"
      @to="go({ to: $event })"
      @hops="setHops"
      @allow-three="allowThreeHops"
      @max-hops="maxHops = $event"
      @kind="kinds = toggled(kinds, $event)"
      @confidence="confidence = toggled(confidence, $event)"
      @extensions="extensions = !extensions"
    />

    <VBanner v-if="failure" icon="circle-x">{{ failure }}</VBanner>
    <div
      v-if="overloads.length > 0"
      class="overloads flex flex-wrap items-center gap-2 border-b border-line bg-surface px-4 py-2 text-xs text-fg-soft"
      role="group"
      aria-label="Overloads"
    >
      Choose one:
      <VButton
        v-for="node in overloads"
        :key="node.id"
        size="sm"
        class="overload font-mono"
        @click="chooseOverload(node)"
      >
        {{ qualified(node) }}
      </VButton>
    </div>

    <div class="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_var(--spacing-lineage-panel)]">
      <main
        class="relative isolate flex min-h-0 min-w-0 flex-col overflow-auto bg-ground"
        @wheel="zoom"
      >
        <!-- The sky behind the chart, a layer of its own painted once when the
             page opens, so no answer's first paint has to paint it again. -->
        <div
          class="pointer-events-none absolute inset-0 -z-10 sky will-change-transform"
          aria-hidden="true"
        />
        <p
          class="m-0 flex flex-none items-center gap-3 px-4 pt-3 pb-1 font-mono text-2xs tracking-hud text-fg-muted uppercase"
        >
          <span>{{ address.database ?? 'No database' }}</span>
          <span v-if="focusNode">focus {{ qualified(focusNode) }}</span>
          <span v-if="address.column">column {{ address.column }} highlighted</span>
          <span v-if="readout" class="readout ml-auto text-fg-soft">{{ readout }}</span>
          <span v-if="snapshot" class="snapshot" :title="fullStamp(snapshot.builtAt)">
            built {{ stamp(snapshot.builtAt) }}<template v-if="snapshot.stale"> · stale</template>
          </span>
          <span v-if="answered?.truncated" class="text-target">truncated</span>
          <VIcon v-if="active.busy.value" name="loader" spin size="sm" label="Reading" />
        </p>

        <LineageOverview
          v-if="address.mode === 'overview'"
          :instance-id="instanceId"
          :database="address.database"
          :extensions="extensions"
          :focus="address.focus"
          @landed="landed"
        />
        <VEmpty v-else-if="address.database === null || address.focus === null">
          Pick a database and an object to see what it is connected to, and which way data moves.
        </VEmpty>
        <div v-else-if="awaiting" class="flex justify-center py-16" data-testid="lineage-awaiting">
          <DelayedLoader size="section" label="Reading the lineage" />
        </div>
        <p
          v-else-if="building"
          class="building m-0 flex items-center justify-center gap-2 px-4 py-16 text-sm text-fg-soft"
          role="status"
          data-testid="lineage-building"
        >
          <VIcon name="loader" spin />
          Lineage for {{ address.database }} is still being built. Asking again
          <span class="font-mono text-fg-muted">({{ active.asked.value }})</span>
        </p>
        <VEmpty v-else-if="address.mode === 'path' && address.to === null" class="choose-to">
          Choose the object to trace a route to.
        </VEmpty>
        <VEmpty
          v-else-if="address.mode === 'path' && path.answer.value && scene === null"
          class="no-route"
        >
          No route joins them within {{ maxHops }} hops.
        </VEmpty>

        <VLineageChart
          v-if="scene && !building"
          class="flex-none"
          :scene="scene"
          :summary="summary"
          :trace-key="address.mode === 'path' ? `${traced}` : ''"
          :selected="address.mode === 'path' ? selectedEdge : null"
          @choose="focusOn"
          @expand="graph.expand($event)"
        >
          <template v-if="dynamicTotal > 0 && address.mode === 'neighbourhood'" #legend>
            <DynamicTray
              :routines="dynamic"
              :total="dynamicTotal"
              :drawn="drawnIds"
              @open="focusOn"
            />
          </template>
        </VLineageChart>
        <VButton
          v-if="address.database !== null && address.mode !== 'overview'"
          size="sm"
          icon="zoom-out"
          class="overview absolute top-9 right-4 bg-surface"
          aria-label="Overview — all schemas"
          title="Overview — all schemas"
          @pointerenter="void loadOverview()"
          @focus="void loadOverview()"
          @click="setMode('overview')"
        />
      </main>

      <aside
        class="min-h-0 overflow-y-auto border-l border-line bg-surface pb-4"
        aria-label="Details"
      >
        <PathPanel
          v-if="address.mode === 'path' && path.answer.value && address.focus && address.to"
          :path="path.answer.value"
          :chosen="chosenRoute"
          :hop="chosenHop"
          :from="refLabel(address.focus)"
          :to="refLabel(address.to)"
          @choose="chooseRoute"
          @hop="chooseHop"
          @open="openFromRoute"
        />
        <template v-else-if="address.mode === 'neighbourhood' && address.database && focusNode">
          <RoutinePanel
            v-if="isRoutine(focusNode.kind)"
            :key="focusNode.id"
            :instance-id="instanceId"
            :database="address.database"
            :routine="focusNode"
            @open="focusOn"
          />
          <ColumnPanel
            v-else-if="focusNode.kind !== 'sequence'"
            :key="focusNode.id"
            :instance-id="instanceId"
            :database="address.database"
            :table="focusNode"
            :column="address.column"
            @pick="go({ column: $event })"
            @open="focusOn"
          />
          <p v-else class="m-0 px-3 py-3 text-sm text-fg-soft">
            A sequence is written by <code>nextval</code>, and read by the defaults drawn beside it.
          </p>
        </template>
        <p v-else class="m-0 px-3 py-3 text-sm text-fg-soft">
          Pick an object. Its columns, or a routine's reads and writes, are listed here.
        </p>
      </aside>
    </div>
  </div>
</template>
