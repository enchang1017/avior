<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'

import { useObjectBrowser, type ObjectRef, type ObjectType } from './useObjectBrowser'
import { describeFailure } from '@/shared/api'
import {
  type SelectOption,
  VBanner,
  VButton,
  VEmpty,
  VIcon,
  VInput,
  VLoader,
  VPageHeader,
  VSelect,
} from '@/components'
import SqlScript from '@/shared/SqlScript.vue'

const props = defineProps<{ objectType: ObjectType }>()

// One instance for now. FEAT-012 turns this into a picker fed by db_instance.
const instanceId = '00000000-0000-0000-0000-0000000000a1'

const browser = useObjectBrowser(instanceId, () => props.objectType)

const bannerText = computed(() => {
  const failure = browser.failure.value
  return failure === null ? null : describeFailure(failure)
})

const qualified = (object: ObjectRef): string => `${object.schema}.${object.name}`

const databaseOptions = computed<SelectOption[]>(() => [
  { value: '', label: 'Choose a database…' },
  ...browser.databases.value.map((name) => ({ value: name, label: name })),
])

const isCurrent = (object: ObjectRef): boolean => {
  const selected = browser.selected.value
  return selected !== null && selected.schema === object.schema && selected.name === object.name
}

async function copy(): Promise<void> {
  const definition = browser.definition.value
  if (definition === null) return
  try {
    await navigator.clipboard.writeText(definition.definition)
  } catch {
    // A refused clipboard is not worth an error banner: the operator can still
    // select the text. Silence here is the whole handling, deliberately.
  }
}

const route = useRoute()
const asked = (key: string): string | null => {
  const value = route.query[key]
  return typeof value === 'string' && value !== '' ? value : null
}

/** A link that names an object is a definition already asked for, from the first paint. */
const linked = ref(asked('database') !== null && asked('schema') !== null && asked('name') !== null)

const reading = computed(
  () =>
    bannerText.value === null &&
    browser.definition.value === null &&
    (linked.value || (browser.busy.value && browser.selected.value !== null)),
)

onMounted(async () => {
  try {
    await browser.loadDatabases()
    const database = asked('database')
    if (database === null) return
    const schema = asked('schema')
    const name = asked('name')
    await browser.openAt(database, schema === null || name === null ? null : { schema, name })
  } finally {
    linked.value = false
  }
})
</script>

<template>
  <div
    class="workspace grid min-h-0 flex-1 grid-cols-[var(--spacing-tree)_minmax(0,1fr)] overflow-hidden"
  >
    <aside
      class="tree flex flex-col gap-3 overflow-hidden border-r border-line bg-surface p-3"
      aria-label="Objects"
    >
      <div class="flex min-h-0 flex-col gap-1.5">
        <label class="eyebrow" :for="'database-' + objectType">Database</label>
        <VSelect
          :id="'database-' + objectType"
          :model-value="browser.database.value ?? ''"
          :options="databaseOptions"
          :disabled="browser.busy.value"
          @update:model-value="browser.chooseDatabase($event || null)"
        />
      </div>

      <div class="flex min-h-0 flex-1 flex-col gap-1.5">
        <label class="eyebrow" :for="'filter-' + objectType">
          {{ browser.label.value }}
          <span class="ml-1.5 font-normal text-fg-muted">{{ browser.visible.value.length }}</span>
        </label>
        <VInput
          :id="'filter-' + objectType"
          v-model="browser.filter.value"
          type="search"
          placeholder="Filter by name"
          :disabled="browser.objects.value.length === 0"
        />

        <p v-if="browser.database.value === null" class="mx-0 mt-2 mb-0 text-xs text-fg-soft">
          Choose a database to list its objects.
        </p>
        <p
          v-else-if="browser.visible.value.length === 0 && !browser.busy.value"
          class="mx-0 mt-2 mb-0 text-xs text-fg-soft"
        >
          Nothing of this type here.
        </p>

        <ul class="mx-0 mt-1 mb-0 min-h-0 list-none overflow-y-auto p-0 text-sm">
          <li v-for="object in browser.visible.value" :key="qualified(object)">
            <button
              type="button"
              class="object flex w-full cursor-pointer items-baseline gap-1.5 rounded-control border-0 px-1.5 py-1 text-left [font:inherit]"
              :class="
                isCurrent(object)
                  ? 'current bg-primary-wash text-primary'
                  : 'bg-transparent text-fg hover:bg-surface-sunken'
              "
              @click="browser.choose(object)"
            >
              <span class="text-xs text-fg-muted" :class="{ 'font-semibold': isCurrent(object) }">{{
                object.schema
              }}</span>
              <span
                class="truncate font-mono text-sm"
                :class="{ 'font-semibold': isCurrent(object) }"
                >{{ object.name }}</span
              >
            </button>
          </li>
        </ul>
      </div>
    </aside>

    <main class="pane flex min-h-0 min-w-0 flex-col overflow-y-auto bg-ground">
      <VPageHeader
        icon="file-code"
        :subtitle="`${browser.label.value} definition`"
        :busy="browser.busy.value"
        busy-label="loading"
      >
        <!-- The name is the heading, not the type. Both the rail and the top bar
             already say which type this is; what the operator needs to read at a
             glance is which object they are looking at. -->
        <template #title>
          <h1
            v-if="browser.selected.value"
            class="name m-0 truncate font-mono text-xl leading-tight font-semibold text-fg"
          >
            <!-- The schema is context, not identity: it stays legible without
                 competing with the name an operator is actually looking for. -->
            <span class="text-code font-normal text-fg-muted"
              >{{ browser.selected.value.schema }}.</span
            >{{ browser.selected.value.name }}
          </h1>
          <h1
            v-else
            class="name m-0 truncate font-mono text-lg leading-tight font-normal text-fg-muted"
          >
            Nothing selected
          </h1>
        </template>

        <VButton :disabled="browser.definition.value === null" @click="copy">
          <VIcon name="copy" />
          Copy
        </VButton>
      </VPageHeader>

      <VBanner v-if="bannerText" icon="circle-x">{{ bannerText }}</VBanner>

      <div v-if="reading" class="flex justify-center py-10">
        <VLoader size="section" label="Reading the definition" />
      </div>
      <VEmpty v-else-if="browser.definition.value === null && bannerText === null">
        Pick a {{ browser.label.value.toLowerCase() }} to read its definition.
      </VEmpty>

      <template v-if="browser.definition.value">
        <SqlScript class="m-3" :code="browser.definition.value.definition" />

        <!-- Kept apart from the script rather than concatenated onto it the way
             the predecessor does, so the object's own text is distinguishable from what
             was granted on it. -->
        <section v-if="browser.definition.value.grants.length > 0" class="grants mt-2">
          <h2 class="mx-3 my-0 eyebrow">Permissions</h2>
          <SqlScript class="m-3" :code="browser.definition.value.grants.join('\n')" />
        </section>
      </template>
    </main>
  </div>
</template>
