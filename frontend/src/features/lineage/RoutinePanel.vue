<script setup lang="ts">
import { computed } from 'vue'
import type { RouteLocationRaw } from 'vue-router'

import { VBadge, VBanner, VButton, VSkeleton } from '@/components'
import ConfidenceBadge from './ConfidenceBadge.vue'
import NotVisible from './NotVisible.vue'
import { nodeLabel, qualified } from './scene'
import type { LineageNode } from './types'
import { useLineageRoutine } from './useLineageRoutine'

const props = defineProps<{ instanceId: string; database: string; routine: LineageNode }>()
const emit = defineEmits<{ open: [id: string] }>()

const footprint = useLineageRoutine(() => ({
  instanceId: props.instanceId,
  database: props.database,
  routine: props.routine.id,
}))
const answer = computed(() => footprint.answer.value)

const badgeWords: Record<string, string> = {
  search_path_assumed: 'search path assumed',
  overload_ambiguous: 'overload ambiguous',
  view_unmapped: 'view unmapped',
  ambiguous: 'ambiguous name',
  dynamic: 'builds SQL at run time',
  may_reach_unknown: 'may reach unknown',
  unparsed: 'unparsed',
  opaque: 'opaque',
}

const badges = computed(() => (answer.value?.routine ?? props.routine).badges ?? [])

const touched = computed(() => [
  ...(answer.value?.writes ?? []).map((entry) => ({ ...entry, access: 'write' as const })),
  ...(answer.value?.reads ?? []).map((entry) => ({ ...entry, access: 'read' as const })),
])

const definition = computed<RouteLocationRaw>(() => ({
  name: props.routine.kind === 'procedure' ? 'objects-stored-procedure' : 'objects-function',
  query: { database: props.database, schema: props.routine.schema, name: props.routine.name },
}))
</script>

<template>
  <section class="routine-panel flex flex-col" data-testid="routine-panel" aria-label="Routine">
    <header class="flex flex-col gap-1.5 border-b border-line px-3 py-3">
      <p class="m-0 eyebrow">
        {{ routine.kind }}<template v-if="answer?.status"> · {{ answer.status }}</template>
      </p>
      <h2 class="m-0 font-mono text-md font-normal break-all text-target">
        {{ qualified(routine) }}
      </h2>
      <div v-if="badges.length > 0" class="flex flex-wrap gap-1">
        <VBadge
          v-for="badge in badges"
          :key="badge"
          size="sm"
          :tone="badge === 'dynamic' || badge === 'may_reach_unknown' ? 'warn' : 'neutral'"
          class="routine-badge"
          :data-badge="badge"
        >
          {{ badgeWords[badge] ?? badge }}
        </VBadge>
      </div>
    </header>

    <VBanner v-if="footprint.failure.value" icon="circle-x">{{ footprint.failure.value }}</VBanner>
    <p v-if="footprint.building.value" class="building m-0 px-3 py-3 text-xs text-fg-soft">
      Lineage for {{ database }} is still being built. Asking again…
    </p>
    <VSkeleton v-else-if="answer === null && !footprint.failure.value" :lines="4" class="px-3" />

    <template v-if="answer">
      <p v-if="answer.message" class="m-0 px-3 pt-3 text-xs text-danger">{{ answer.message }}</p>
      <VBanner v-if="answer.mayReachUnknown" tone="warn" :alert="false" icon="warning">
        It calls a routine that builds SQL at run time, so it may reach objects nobody can list.
      </VBanner>

      <section class="footprint px-3 pt-3" aria-label="Reads and writes">
        <h3 class="m-0 pb-1 eyebrow">Reads and writes</h3>
        <p v-if="touched.length === 0" class="m-0 text-xs text-fg-muted">
          It reads and writes no table the parser could name.
        </p>
        <ul class="m-0 list-none p-0">
          <li
            v-for="entry in touched"
            :key="`${entry.access}:${entry.table.id}`"
            class="touch flex items-start gap-2 border-b border-line py-1.5"
            :data-access="entry.access"
          >
            <span
              class="mt-0.5 w-4 text-center font-mono text-xs"
              :class="entry.access === 'write' ? 'text-lineage-write' : 'text-lineage-read'"
              >{{ entry.access === 'write' ? 'W' : 'R' }}</span
            >
            <div class="flex min-w-0 flex-1 flex-col">
              <span class="truncate font-mono text-sm text-fg">{{ qualified(entry.table) }}</span>
              <span class="flex flex-wrap gap-x-2 font-mono text-2xs text-fg-soft">
                <span
                  v-for="column in entry.columns"
                  :key="column.name"
                  :class="column.restricted ? 'restricted text-danger' : ''"
                  >{{ column.name
                  }}<template v-if="column.restricted"> (restricted)</template></span
                >
              </span>
            </div>
            <ConfidenceBadge :confidence="entry.confidence" />
          </li>
        </ul>
      </section>

      <section
        v-if="(answer.statements ?? []).length > 0"
        class="px-3 pt-3"
        aria-label="Statements"
      >
        <h3 class="m-0 pb-1 eyebrow">Statements</h3>
        <ul class="m-0 list-none p-0 font-mono text-2xs text-fg-soft">
          <li
            v-for="statement in answer.statements"
            :key="statement.line"
            class="statement flex gap-2 border-b border-line py-1"
          >
            <span class="w-8 flex-none text-fg-muted">{{ statement.line }}</span>
            <span class="min-w-0 break-all">{{ statement.excerpt }}</span>
          </li>
        </ul>
      </section>

      <section class="calls px-3 pt-3" aria-label="Calls">
        <h3 class="m-0 pb-1 eyebrow">Calls</h3>
        <p
          v-if="(answer.calls ?? []).length + (answer.transitiveCalls ?? []).length === 0"
          class="m-0 text-xs text-fg-muted"
        >
          It calls no other routine.
        </p>
        <ul class="m-0 flex list-none flex-wrap gap-1.5 p-0">
          <li v-for="call in answer.calls ?? []" :key="`d:${call.id}`">
            <VButton variant="bare" size="sm" class="call" @click="emit('open', call.id)">
              {{ nodeLabel(call) }}
            </VButton>
          </li>
          <li v-for="call in answer.transitiveCalls ?? []" :key="`t:${call.id}`" class="transitive">
            <VButton variant="bare" size="sm" class="call" @click="emit('open', call.id)">
              <span class="text-fg-muted">via</span> {{ nodeLabel(call) }}
            </VButton>
          </li>
        </ul>
      </section>

      <section class="fires px-3 pt-3" aria-label="Fires">
        <h3 class="m-0 pb-1 eyebrow">Fires</h3>
        <p v-if="(answer.triggers ?? []).length === 0" class="m-0 text-xs text-fg-muted">
          Its writes fire no trigger.
        </p>
        <ul class="m-0 list-none p-0 text-xs">
          <li v-for="fired in answer.triggers ?? []" :key="fired.name" class="fired py-0.5">
            <span class="font-mono text-fg">{{ fired.name }}</span>
            <span class="text-fg-muted"> on {{ fired.table.name }} → </span>
            <span class="font-mono text-fg-soft">{{ nodeLabel(fired.function) }}</span>
          </li>
        </ul>
      </section>

      <section
        v-if="(answer.dynamic ?? []).length + (answer.unmodelled ?? []).length > 0"
        class="sites px-3 pt-3"
        aria-label="Built at run time"
      >
        <h3 class="m-0 pb-1 eyebrow">Not drawn</h3>
        <ul class="m-0 list-none p-0">
          <li
            v-for="site in [...(answer.dynamic ?? []), ...(answer.unmodelled ?? [])]"
            :key="`${site.form}:${site.line}`"
            class="site flex items-start gap-2 border-b border-line py-1.5 font-mono text-2xs"
          >
            <span class="w-8 flex-none text-fg-muted">{{ site.line }}</span>
            <span class="min-w-0 flex-1 break-all text-fg-soft">
              {{ site.form }}<template v-if="site.excerpt"> · {{ site.excerpt }}</template>
            </span>
            <ConfidenceBadge v-if="(answer.dynamic ?? []).includes(site)" confidence="dynamic" />
          </li>
        </ul>
      </section>

      <section
        v-if="(answer.unresolved ?? []).length > 0"
        class="px-3 pt-3"
        aria-label="Unresolved"
      >
        <h3 class="m-0 pb-1 eyebrow">Unresolved</h3>
        <p class="m-0 font-mono text-2xs text-fg-soft">
          {{ (answer.unresolved ?? []).join(', ') }}
        </p>
      </section>
    </template>

    <p
      class="m-3 mb-0 rounded-card border border-line bg-surface-sunken px-3 py-2 text-xs text-fg-soft"
    >
      The definition opens in the object viewer, which records the read (FEAT-023).
      <VButton variant="bare" size="sm" :to="definition" class="open-definition text-primary">
        Open definition
      </VButton>
    </p>
    <NotVisible />
  </section>
</template>
