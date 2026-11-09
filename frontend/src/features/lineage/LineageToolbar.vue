<script setup lang="ts">
import type { Mode } from './address'
import { computed, watch } from 'vue'

import { VButton, VFilterSelect } from '@/components'
import { lazyPart } from './lazyPart'
import type { Hops } from './address'
import { columnOwner, focusSuggestions, resolveFocusText, type FocusPick } from './focusText'
import LineageChip from './LineageChip.vue'
import PendingSelect from './PendingSelect.vue'
import {
  confidences,
  nodeKinds,
  type FocusRef,
  type LineageConfidence,
  type LineageNodeKind,
} from './types'
import { useTypedPick } from './useTypedPick'

// Only path mode draws these, so only path mode fetches them.
const PathControls = lazyPart(() => import('./PathControls.vue'), 'Loading path controls', 'inline')

const props = defineProps<{
  databases: readonly string[]
  database: string | null
  /** What the pickers can name, the text each shows, and the columns known of a table. */
  focusRefs: readonly FocusRef[]
  focusValue: string
  toValue: string
  columnsOf: (ref: FocusRef) => readonly string[] | undefined
  mode: Mode
  hops: Hops
  allowThree: boolean
  maxHops: number
  kinds: ReadonlySet<LineageNodeKind>
  confidence: ReadonlySet<LineageConfidence>
  extensions: boolean
  extensionsHidden: number
  /** The databases, then the objects of the chosen one, are still being listed. */
  databasesPending?: boolean
  objectsPending?: boolean
}>()

const emit = defineEmits<{
  database: [name: string | null]
  pickFocus: [pick: FocusPick | null]
  needColumns: [ref: FocusRef]
  mode: [mode: Mode]
  to: [ref: FocusRef | null]
  hops: [hops: Hops]
  allowThree: [allowed: boolean]
  maxHops: [hops: number]
  kind: [kind: LineageNodeKind]
  confidence: [confidence: LineageConfidence]
  extensions: []
}>()

const database = useTypedPick(
  () => props.database ?? '',
  (text) =>
    props.databases.filter((name) => name.toLowerCase().includes(text.trim().toLowerCase())),
  (text) => {
    const found = props.databases.find((name) => name.toLowerCase() === text.trim().toLowerCase())
    if (text.trim() === '' || found !== undefined) emit('database', found ?? null)
    return text.trim() === '' || found !== undefined
  },
)

const focus = useTypedPick(
  () => props.focusValue,
  (text) => focusSuggestions(text, props.focusRefs, props.columnsOf),
  (text) => {
    const pick = resolveFocusText(text, props.focusRefs, props.columnsOf)
    if (text.trim() === '' || pick !== null) emit('pickFocus', pick)
    return text.trim() === '' || pick !== null
  },
)
watch(focus.typed, (text) => {
  const owner = columnOwner(text.trim(), props.focusRefs)
  if (owner !== null) emit('needColumns', owner)
})

const kindLabels: Record<LineageNodeKind, string> = {
  table: 'Tables',
  view: 'Views',
  matview: 'Mat. views',
  function: 'Functions',
  procedure: 'Procedures',
  sequence: 'Sequences',
}

const hopChoices = computed<Hops[]>(() => (props.allowThree ? [1, 2, 3] : [1, 2]))

const segment = (on: boolean) =>
  on
    ? 'border-primary-soft bg-primary-wash text-primary'
    : 'border-line bg-transparent text-fg-soft hover:text-fg'

const modeWords: Record<Exclude<Mode, 'overview'>, string> = {
  neighbourhood: 'Neighbourhood',
  path: 'Path to…',
}
</script>

<template>
  <div
    class="lineage-toolbar flex flex-none flex-wrap items-center gap-x-3 gap-y-2 border-b border-line bg-surface px-4 py-2"
  >
    <div class="flex items-center gap-2">
      <label class="eyebrow" for="lineage-database">Database</label>
      <div class="w-44">
        <PendingSelect v-if="databasesPending" label="Loading databases" />
        <VFilterSelect
          v-else
          id="lineage-database"
          v-model="database.typed.value"
          size="sm"
          :options="database.options.value"
          :match-typed="false"
          empty-label="Choose a database…"
          @pick="database.pick"
        />
      </div>
      <span v-if="database.unresolved.value" class="unresolved text-xs text-danger" role="status">
        No database by that name
      </span>
    </div>

    <div class="flex items-center gap-2">
      <label class="eyebrow" for="lineage-focus">{{ mode === 'path' ? 'From' : 'Focus' }}</label>
      <div class="w-72">
        <PendingSelect v-if="objectsPending" label="Loading objects" />
        <VFilterSelect
          v-else
          id="lineage-focus"
          v-model="focus.typed.value"
          size="sm"
          :options="focus.options.value"
          :match-typed="false"
          empty-label="Choose an object…"
          placeholder="table, table.column or a routine"
          @pick="focus.pick"
        />
      </div>
      <span v-if="focus.unresolved.value" class="unresolved text-xs text-danger" role="status">
        No object or column by that name
      </span>
    </div>

    <div class="flex" role="group" aria-label="Mode">
      <button
        v-for="choice in ['neighbourhood', 'path'] as const"
        :key="choice"
        type="button"
        class="mode h-control-sm cursor-pointer border px-2.5 font-[inherit] text-xs ease-colours first:rounded-l-control last:rounded-r-control"
        :class="segment(mode === choice)"
        :aria-pressed="mode === choice"
        @click="emit('mode', choice)"
      >
        {{ modeWords[choice] }}
      </button>
    </div>

    <PathControls
      v-if="mode === 'path'"
      :focus-refs="focusRefs"
      :to-value="toValue"
      :max-hops="maxHops"
      :pending="objectsPending === true"
      @to="emit('to', $event)"
      @max-hops="emit('maxHops', $event)"
    />

    <template v-else-if="mode === 'neighbourhood'">
      <div class="flex items-center gap-2" role="group" aria-label="Hops">
        <span class="eyebrow">Hops</span>
        <div class="flex">
          <button
            v-for="choice in hopChoices"
            :key="choice"
            type="button"
            class="hop h-control-sm w-7 cursor-pointer border font-mono text-xs ease-colours first:rounded-l-control last:rounded-r-control"
            :class="segment(hops === choice)"
            :aria-pressed="hops === choice"
            @click="emit('hops', choice)"
          >
            {{ choice }}
          </button>
        </div>
        <VButton
          v-if="!allowThree"
          variant="bare"
          size="sm"
          class="allow-three"
          @click="emit('allowThree', true)"
        >
          Allow 3 hops
        </VButton>
      </div>

      <div class="flex flex-wrap items-center gap-1" role="group" aria-label="Object kinds">
        <LineageChip
          v-for="kind in nodeKinds"
          :key="kind"
          :pressed="kinds.has(kind)"
          :data-kind="kind"
          @toggle="emit('kind', kind)"
        >
          {{ kindLabels[kind] }}
        </LineageChip>
      </div>

      <div class="flex items-center gap-1" role="group" aria-label="Confidence">
        <LineageChip
          v-for="level in confidences"
          :key="level"
          :pressed="confidence.has(level)"
          :data-confidence="level"
          @toggle="emit('confidence', level)"
        >
          {{ level === 'exact' ? 'Exact' : 'Parsed' }}
        </LineageChip>
      </div>

      <LineageChip :pressed="extensions" class="extensions" @toggle="emit('extensions')">
        Extension routines
        <span v-if="!extensions && extensionsHidden > 0" class="font-mono text-2xs"
          >{{ extensionsHidden }} hidden</span
        >
      </LineageChip>
    </template>
  </div>
</template>
