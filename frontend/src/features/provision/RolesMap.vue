<script setup lang="ts">
/**
 * FEAT-076 AC-17. The roles about one database in three columns — the
 * database, its owner and group roles, the logins — joined by thin lines.
 *
 * Static on purpose: a provisioning page is read, not explored. The lines are
 * measured off the cards after they are drawn, and again whenever a column
 * scrolls or the page changes size, so they stay on the cards they join.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import { VBadge, VConnectors, type Connector } from '@/components'
import type { ProvisionedRole } from './useRoles'
import { emphasis, laneTurns, rolesMap, type MapCard } from './rolesMap'

const props = defineProps<{ roles: ProvisionedRole[]; database: string; chosen: string | null }>()
const emit = defineEmits<{ choose: [name: string] }>()

const map = computed(() => rolesMap(props.roles, props.database))

const root = ref<HTMLElement | null>(null)
const lines = ref<Connector[]>([])

/** Where a card's edge is, held inside the column it scrolls in. */
function anchor(id: string, side: 'left' | 'right', origin: DOMRect) {
  const card = root.value?.querySelector<HTMLElement>(`[data-card="${CSS.escape(id)}"]`)
  if (card === null || card === undefined) return null
  const box = card.getBoundingClientRect()
  const column = card.closest<HTMLElement>('[data-column]')?.getBoundingClientRect() ?? box
  const y = Math.min(Math.max(box.top + box.height / 2, column.top), column.bottom)
  return {
    x: Math.round((side === 'right' ? box.right : box.left) - origin.left),
    y: Math.round(y - origin.top),
  }
}

/** The card pointed at, focused, or chosen, whose lines are drawn lit. */
const pointed = ref<string | null>(null)
const active = computed(() => pointed.value ?? props.chosen)

const placed = ref<(Connector & { from: string; to: string })[]>([])

function measure(): void {
  const origin = root.value?.getBoundingClientRect()
  if (origin === undefined) return
  const drawn = map.value.links.flatMap((link) => {
    const start = anchor(link.from, 'right', origin)
    const end = anchor(link.to, 'left', origin)
    if (start === null || end === null) return []
    return [
      {
        key: `${link.from}>${link.to}`,
        x1: start.x,
        y1: start.y,
        x2: end.x,
        y2: end.y,
        dashed: link.dashed,
        from: link.from,
        to: link.to,
      },
    ]
  })
  const turns = laneTurns(drawn)
  placed.value = drawn.map((line) => ({ ...line, turn: turns.get(line.key) ?? line.x1 }))
}

watch(
  [placed, active],
  ([drawn, id]) => {
    lines.value = drawn.map(({ from, to, ...line }) => {
      const state = emphasis({ from, to, dashed: line.dashed === true }, id)
      return state === undefined ? line : { ...line, emphasis: state }
    })
  },
  { immediate: true },
)

let resizing: ResizeObserver | null = null
onMounted(() => {
  void nextTick(measure)
  if (typeof ResizeObserver === 'undefined' || root.value === null) return
  resizing = new ResizeObserver(() => measure())
  resizing.observe(root.value)
})
onBeforeUnmount(() => resizing?.disconnect())
watch(map, () => void nextTick(measure))

function cardClass(card: MapCard): string[] {
  const chosenNow = props.chosen === card.id
  const ground = card.warn ? 'bg-warn-bg' : chosenNow ? 'bg-primary-wash' : 'bg-surface'
  return [
    card.warn ? 'border-warn-line' : 'border-line',
    ground,
    chosenNow ? 'border-l-3 border-l-primary' : '',
  ]
}

const column = 'flex max-h-dialog-body-max flex-col gap-2 overflow-y-auto pr-1'
const heading = 'm-0 mb-2 eyebrow text-2xs text-fg-muted'
const card =
  'flex w-full cursor-pointer flex-col items-start gap-1 rounded-card border px-3 py-2 text-left text-fg hover:bg-surface-raised focus-visible:outline-2 focus-visible:outline-primary'
</script>

<template>
  <div class="flex flex-col gap-3" data-testid="roles-map">
    <div ref="root" class="relative grid grid-cols-3 gap-16">
      <VConnectors :lines="lines" />

      <section>
        <h3 :class="heading">Database</h3>
        <div :class="column" data-column="database">
          <div
            class="rounded-card border border-line bg-surface px-3 py-2"
            :data-card="map.database.id"
            data-testid="roles-map-database"
          >
            <code class="font-mono text-sm font-semibold">{{ map.database.name }}</code>
          </div>
        </div>
      </section>

      <section>
        <h3 :class="heading">Owner &amp; group roles</h3>
        <div :class="column" data-column="roles" @scroll.passive="measure">
          <button
            v-for="role in map.roles"
            :key="role.id"
            type="button"
            :class="[card, ...cardClass(role)]"
            :data-card="role.id"
            :data-role="role.name"
            :data-connect="role.canConnect ? 'yes' : 'no'"
            :aria-pressed="chosen === role.id"
            @click="emit('choose', role.name)"
            @mouseenter="pointed = role.id"
            @mouseleave="pointed = null"
            @focus="pointed = role.id"
            @blur="pointed = null"
          >
            <span class="flex w-full items-center gap-2">
              <code class="truncate font-mono text-sm font-semibold">{{ role.name }}</code>
              <VBadge size="sm">{{ role.kind }}</VBadge>
            </span>
            <span class="text-2xs" :class="role.warn ? 'text-warn' : 'text-fg-muted'">{{
              role.status
            }}</span>
          </button>
        </div>
      </section>

      <section>
        <h3 :class="heading">Logins</h3>
        <div :class="column" data-column="logins" @scroll.passive="measure">
          <p v-if="map.logins.length === 0" class="m-0 text-sm text-fg-muted">
            No login reaches {{ database }}.
          </p>
          <button
            v-for="login in map.logins"
            :key="login.id"
            type="button"
            :class="[card, ...cardClass(login)]"
            :data-card="login.id"
            :data-role="login.kind === 'login' ? login.name : undefined"
            :data-more="login.group"
            :data-connect="login.canConnect ? 'yes' : 'no'"
            :aria-pressed="chosen === login.id"
            @click="emit('choose', login.group ?? login.name)"
            @mouseenter="pointed = login.id"
            @mouseleave="pointed = null"
            @focus="pointed = login.id"
            @blur="pointed = null"
          >
            <span class="flex w-full items-center gap-2">
              <code class="truncate font-mono text-sm font-semibold">{{ login.name }}</code>
              <VBadge v-if="login.kind === 'login'" size="sm">login</VBadge>
            </span>
            <span v-if="login.warn" class="text-2xs text-warn">{{ login.status }}</span>
            <span v-else-if="login.kind === 'more'" class="text-2xs text-fg-muted">{{
              login.status
            }}</span>
          </button>
        </div>
      </section>
    </div>

    <p class="m-0 font-mono text-2xs text-fg-muted" aria-label="Legend">
      ─ member of / may connect &nbsp;&nbsp; ┄ has privileges, no CONNECT
    </p>
  </div>
</template>
