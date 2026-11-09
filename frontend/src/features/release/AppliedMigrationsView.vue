<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  useAppliedMigrations,
  statusTone,
  statusMeaning,
  type AppliedMigration,
} from './useAppliedMigrations'
import { useReleaseQueue } from './useReleaseQueue'
import {
  VBadge,
  VBanner,
  VCard,
  VEmpty,
  VField,
  VFilterBar,
  VFilterSelect,
  VPageHeader,
  VSkeleton,
} from '@/components'
import SqlScript from '@/shared/SqlScript.vue'
import { describeFailure } from '@/shared/api'
import { fullStamp, stamp } from '@/shared/timestamp'

const route = useRoute()
const router = useRouter()

const history = useAppliedMigrations()

// The databases to offer come from the ledger rather than from a list Avior
// keeps: a database nothing was ever released to has no history to read.
const ledger = useReleaseQueue(() => 'history')
const databases = computed(() => [
  ...new Set(ledger.releases.value.map((release) => release.database)),
])

const bannerText = computed(() => {
  const failure = history.failure.value
  return failure === null ? null : describeFailure(failure)
})

function appliedAt(migration: AppliedMigration): string {
  if (migration.appliedAt === null || migration.appliedAt === undefined) return 'never'
  return stamp(migration.appliedAt)
}

// The URL carries the question so a link to it asks the same one again.
async function apply() {
  await router.replace({
    name: 'release-applied',
    query: {
      ...(history.database.value === '' ? {} : { database: history.database.value }),
      ...(history.table.value.trim() === '' ? {} : { table: history.table.value.trim() }),
    },
  })
  await history.load()
}

function readQuery() {
  history.database.value =
    typeof route.query['database'] === 'string' ? route.query['database'] : ''
  history.table.value = typeof route.query['table'] === 'string' ? route.query['table'] : ''
}

watch(databases, (available) => {
  if (history.database.value === '' && available.length > 0) {
    history.database.value = available[0] ?? ''
    void history.loadTables()
    void history.load()
  }
})

// The names on offer belong to the database, so they are re-read when it
// changes and not when the filter does.
watch(
  () => history.database.value,
  () => {
    history.table.value = ''
    void history.loadTables()
  },
)

onMounted(async () => {
  readQuery()
  await Promise.all([ledger.load(), history.load(), history.loadTables()])
})
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-ground">
    <VPageHeader icon="layers" title="What reached this database" :busy="history.busy.value" />

    <VFilterBar :busy="history.busy.value" @submit="apply()">
      <VField label="Database" for="applied-database">
        <VFilterSelect
          id="applied-database"
          v-model="history.database.value"
          :options="databases"
          empty-label="choose one"
        />
      </VField>
      <VField label="Table" for="applied-table" wide>
        <!--
          The names come from the migrations themselves, so a database with
          hundreds needs typing to be useful. A migration whose SQL could not be
          read names nothing, so it is not in this list and still appears under
          every filter.
        -->
        <VFilterSelect
          id="applied-table"
          v-model="history.table.value"
          :options="history.tables.value"
          empty-label="every table"
        />
      </VField>
    </VFilterBar>

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>
    <VBanner v-if="history.unreadable.value">{{ history.unreadable.value }}</VBanner>
    <VBanner v-if="history.forbidden.value">
      Reading this needs the permission that governs reading releases.
    </VBanner>

    <VBanner v-if="history.problems.value.length > 0" tone="warn" :alert="false">
      {{ history.problems.value.length }} of {{ history.migrations.value.length }} rows are a
      disagreement between the two ledgers. Avior reports these; deciding which side is right needs
      somebody who knows what was done outside the pipeline.
    </VBanner>

    <div class="rows min-h-0 flex-1 overflow-auto p-3">
      <!-- The status is the first thing read on this page, so a disagreement is
           told from an ordinary row by colour rather than by reading the word. -->
      <ul
        v-if="history.migrations.value.length > 0"
        class="list m-0 flex list-none flex-col gap-2 p-0"
      >
        <VCard
          v-for="migration in history.migrations.value"
          :key="migration.filename"
          as="li"
          class="entry flex flex-col gap-1.5 px-3 py-2.5"
        >
          <div class="flex flex-wrap items-center gap-1.5">
            <VBadge :tone="statusTone[migration.status]">
              {{ migration.status }}
            </VBadge>
            <span class="filename font-mono text-sm font-semibold">{{ migration.filename }}</span>
            <span class="text-xs text-fg-soft" :title="fullStamp(migration.appliedAt ?? '')">{{
              appliedAt(migration)
            }}</span>
            <VBadge v-if="!migration.relationsKnown" title="This file could not be read">
              relations unknown
            </VBadge>
          </div>

          <p v-if="migration.status !== 'recorded'" class="max-w-2xl text-xs text-fg-soft">
            {{ statusMeaning[migration.status] }}
          </p>

          <p v-if="migration.release" class="text-xs text-fg-soft">
            {{ migration.release.author }} · {{ migration.release.commitSha.slice(0, 10) }}
            <a
              v-if="migration.release.mergeRequestUrl"
              class="underline"
              :href="migration.release.mergeRequestUrl"
            >
              merge request
            </a>
          </p>

          <p v-if="migration.status === 'mismatched'" class="checksums font-mono text-xs">
            database {{ migration.targetChecksum?.slice(0, 12) }} · avior
            {{ migration.ledgerChecksum?.slice(0, 12) }}
          </p>

          <p v-if="migration.relations && migration.relations.length > 0" class="font-mono text-xs">
            {{ migration.relations.join(', ') }}
          </p>

          <SqlScript v-if="migration.sql" :code="migration.sql" />
          <p v-else class="text-xs text-fg-soft">
            No SQL: Avior never saw this file, which is not the same as a file that was empty.
          </p>
        </VCard>
      </ul>

      <VSkeleton v-else-if="history.busy.value" :lines="6" label="Reading the history" />

      <VEmpty v-else-if="history.database.value === ''"> Choose a database. </VEmpty>
      <VEmpty v-else-if="history.loaded.value">
        Nothing has been applied to this database through Avior.
      </VEmpty>
    </div>
  </div>
</template>
