<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'

import { VBanner, VEmpty, VIcon } from '@/components'
import VLineageGalaxy from '@/components/Data/VLineageGalaxy/VLineageGalaxy.vue'
import DelayedLoader from '@/shared/DelayedLoader.vue'
import type { FocusRef, LineageNode } from './types'
import { useLineageOverview } from './useLineageOverview'

/**
 * The galaxy overview (FEAT-070 AC-53 and AC-54): the database at the scale of
 * its schemas, and the camera that flies to one. A chunk of its own, so a
 * neighbourhood never pays for it.
 */
const props = defineProps<{
  instanceId: string
  database: string | null
  extensions: boolean
  /** An object chosen while the overview shows, which the camera flies to. */
  focus: FocusRef | null
}>()

const emit = defineEmits<{ landed: [anchor: LineageNode | null] }>()

const overview = useLineageOverview(() =>
  props.database === null
    ? null
    : { instanceId: props.instanceId, database: props.database, extensions: props.extensions },
)
const schemas = computed(() =>
  (overview.answer.value?.schemas ?? []).map(({ schema, objects }) => ({ schema, objects })),
)
const links = computed(() => overview.answer.value?.links ?? [])
const summary = computed(() => {
  const clusters = overview.answer.value?.schemas ?? []
  const objects = clusters.reduce((sum, cluster) => sum + cluster.objects, 0)
  return `${clusters.length} schemas holding ${objects} objects, and ${links.value.length} links between them.`
})

const flyTo = ref<string | null>(null)
const landingOn = shallowRef<LineageNode | null>(null)

/** A cluster picked: the camera flies to it and lands on its most connected object. */
function pick(schema: string): void {
  landingOn.value = overview.answer.value?.schemas?.find((c) => c.schema === schema)?.anchor ?? null
  flyTo.value = schema
}

watch(
  () => props.focus,
  (focus) => {
    if (focus !== null && flyTo.value === null) {
      landingOn.value = null
      flyTo.value = focus.schema
    }
  },
)

function flown(): void {
  const anchor = landingOn.value
  flyTo.value = null
  landingOn.value = null
  emit('landed', anchor)
}
</script>

<template>
  <VEmpty v-if="database === null">Pick a database to see its schemas as a galaxy.</VEmpty>
  <VBanner v-else-if="overview.failure.value" icon="circle-x">{{ overview.failure.value }}</VBanner>
  <VLineageGalaxy
    v-else-if="schemas.length > 0"
    class="flex-none"
    :schemas="schemas"
    :links="links"
    :summary="summary"
    :fly-to="flyTo"
    @pick="pick"
    @flown="flown"
  />
  <div
    v-else-if="overview.answer.value === null && !overview.building.value"
    class="flex justify-center py-16"
    data-testid="overview-awaiting"
  >
    <DelayedLoader size="section" label="Reading the overview" />
  </div>
  <p
    v-else-if="overview.building.value"
    class="building m-0 flex items-center justify-center gap-2 px-4 py-16 text-sm text-fg-soft"
    role="status"
  >
    <VIcon name="loader" spin />
    Lineage for {{ database }} is still being built. Asking again
  </p>
</template>
