<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { needsColumns, useWriteComposer, writeKinds } from './useWriteComposer'
import { useWriteRequests } from './useWriteRequests'
import { VBanner, VButton, VField, VIcon, VInput, VSelect, VSql, VToolbar } from '@/components'
import SchemaTree from '@/features/schema/SchemaTree.vue'
import { useSchemaBrowser } from '@/features/schema/useSchemaBrowser'
import GuardChip from '@/features/query/GuardChip.vue'
import { writeRules } from '@/features/query/guardRules'
import { describeFailure, type TableRef } from '@/shared/api'
import { lazy } from '@/shared/lazy'
import { label as vimLabel, useVimMode } from '@/shared/vimMode'

/*
 * The editor is fetched as the page mounts rather than with it: a route other
 * than the query console is held to 90 KB on first load (ADR 0009 P3), and the
 * editor alone is more than that. The page draws, then the editor arrives.
 */
const SqlEditor = lazy(() => import('@/features/query/SqlEditor.vue'), 'Loading the editor')

/**
 * FEAT-009's composer, laid out like the query console because it is the same
 * job against a different verb: pick the table, pick the columns, and the
 * statement's skeleton is written for you.
 *
 * The one thing here that the console has no equivalent of is Check. A write is
 * a request somebody else has to read, so seeing how many rows it would touch
 * before asking them is worth a round trip — and a refusal found here costs a
 * request nobody had to look at.
 */

// One instance for now. FEAT-012 turns this into a picker fed by db_instance.
const instanceId = '00000000-0000-0000-0000-0000000000a1'

const route = useRoute()
const router = useRouter()
const vim = useVimMode()
const browser = useSchemaBrowser(instanceId)
const composer = useWriteComposer()
const requests = useWriteRequests()

const database = computed(() => browser.database.value ?? '')
const table = computed(() => browser.table.value)

const canCheck = computed(
  () => database.value !== '' && composer.statement.value.trim() !== '' && !composer.busy.value,
)

// Checked and allowed. Submitting an unchecked statement is possible — the
// server evaluates it again either way — but the button reads as the last step
// rather than as a second guess, so it waits for the answer.
const canSubmit = computed(
  () => composer.allowed.value && composer.reason.value.trim() !== '' && !requests.busy.value,
)

const bannerText = computed(() => {
  const failure = composer.failure.value ?? browser.failure.value ?? requests.failure.value
  return failure === null || failure === undefined ? null : describeFailure(failure)
})

const insertLabel = computed(
  () => writeKinds.find((entry) => entry.value === composer.kind.value)?.label ?? 'Insert',
)

/** What the editor completes from: the table in hand and its readable columns. */
const catalogue = computed(() => {
  const current = table.value
  if (current === null) return {}
  const readable = browser.columns.value
    .filter((column) => !column.denied)
    .map((column) => column.name)
  return { [current.schema]: { [current.name]: readable } }
})

async function insert(): Promise<void> {
  const current = table.value
  if (current === null) return
  await composer.template(current.schema, current.name, browser.selectedColumns.value)
}

/**
 * Picking a table writes the skeleton for it, the way the console writes a
 * SELECT. Making somebody press the button to get the thing they just asked for
 * is a step between them and the job.
 */
async function chooseTable(next: TableRef | null): Promise<void> {
  await browser.chooseTable(next)
  if (next !== null) await insert()
}

// Changing the verb changes the statement, so it rewrites rather than leaving
// an UPDATE skeleton on screen under a DELETE heading.
async function chooseKind(next: string): Promise<void> {
  composer.kind.value = next as (typeof writeKinds)[number]['value']
  if (table.value !== null) await insert()
}

async function submit(): Promise<void> {
  const made = await requests.submit(
    database.value,
    composer.statement.value,
    composer.reason.value,
  )
  if (made === null) return
  await router.push({ name: 'write-requests' })
}

/**
 * Start from a request somebody already made.
 *
 * Re-applying a change that was undone is a new change to production, so it
 * goes through an approval like any other — but nobody should have to
 * reconstruct the statement to ask for it. `?from=<id>` fills the composer with
 * what that request said, and the operator edits and submits it as their own.
 */
async function startFrom(id: string): Promise<void> {
  await requests.open(id)
  const from = requests.detail.value?.request
  if (from === undefined) return

  composer.kind.value = from.kind
  composer.statement.value = from.statement
  composer.reason.value = ''
  composer.edited()
  await browser.chooseDatabase(from.database)
  requests.close()
}

const reasons = 'm-0 flex list-none flex-col gap-1 p-0'
const reasonRow = 'flex items-baseline gap-2'
const refused = 'border-l-3 border-l-danger pl-3'

onMounted(() => {
  void browser.loadDatabases()
  const from = route.query['from']
  if (typeof from === 'string' && from !== '') void startFrom(from)
})
</script>

<template>
  <div class="grid min-h-0 flex-1 grid-cols-[var(--spacing-tree)_minmax(0,1fr)] overflow-hidden">
    <SchemaTree
      mode="write"
      :insert-label="insertLabel"
      :columns-optional="!needsColumns(composer.kind.value)"
      :databases="browser.databases.value"
      :tables="browser.tables.value"
      :columns="browser.columns.value"
      :database="browser.database.value"
      :table="browser.table.value"
      :selected="browser.selected.value"
      :all-selected="browser.allSelected.value"
      :busy="browser.busy.value"
      :order-by="''"
      :descending="false"
      :orderable-columns="[]"
      :temporal-columns="[]"
      @choose-database="browser.chooseDatabase"
      @choose-table="chooseTable"
      @toggle="browser.toggle"
      @toggle-all="browser.toggleAll"
      @insert="insert"
    />

    <main class="flex min-h-0 min-w-0 flex-col overflow-hidden bg-surface">
      <VToolbar :busy="composer.busy.value" busy-label="checking">
        <template #lead>
          <VButton :disabled="!canCheck" @click="composer.check(database)">
            <VIcon name="scan" />
            Check
          </VButton>
          <VButton variant="primary" :disabled="!canSubmit" @click="submit()">
            <VIcon name="send" />
            Ask for approval
          </VButton>
          <GuardChip :rules="writeRules" />
        </template>

        <VField label="Statement" for="write-kind">
          <VSelect
            id="write-kind"
            size="sm"
            :model-value="composer.kind.value"
            :options="writeKinds"
            @update:model-value="chooseKind($event)"
          />
        </VField>
      </VToolbar>

      <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

      <!--
        The editor keeps a floor, and the answer below it gets a ceiling.

        `min-h-0` lets a flex item shrink below its content, which is what the
        editor needs to scroll rather than push the pane — and it also lets it
        shrink to nothing. Checking a statement grew the panel underneath by a
        table's worth of columns and the editor collapsed to zero, so the thing
        being written disappeared the moment it was checked.
      -->
      <div class="editor-holder flex min-h-40 flex-auto flex-col overflow-hidden">
        <SqlEditor
          v-model="composer.statement.value"
          :schema="catalogue"
          :default-table="browser.table.value?.name ?? ''"
          :default-schema="browser.table.value?.schema ?? ''"
          :vim-enabled="vim.enabled.value"
          @update:model-value="composer.edited()"
          @update:vim-mode="vim.mode.value = $event"
        />
        <div
          class="modeline flex h-7 items-center justify-end gap-2 border-t border-line bg-surface-sunken px-2"
        >
          <span class="mode font-mono text-xs font-semibold text-target">{{
            vimLabel(vim.mode.value)
          }}</span>
          <VButton
            size="sm"
            variant="bare"
            :icon="vim.enabled.value ? 'square-check' : 'square'"
            :title="
              vim.enabled.value
                ? 'Turn Vim keybindings off. Remembered in this browser.'
                : 'Vim keybindings, remembered in this browser.'
            "
            @click="vim.enabled.value = !vim.enabled.value"
          >
            Vim
          </VButton>
        </div>
      </div>

      <!-- Never more than half the pane, and it scrolls rather than growing: the
           reverse statement for a wide table is forty lines, and the editor is
           what the operator is working in. -->
      <section
        class="flex max-h-1/2 min-h-0 flex-initial flex-col gap-3 overflow-y-auto border-t border-line bg-surface-sunken p-3"
      >
        <!-- Before anything has been checked, the panel says what Check is for
             rather than sitting empty and looking broken. -->
        <p v-if="!composer.checked.value" class="m-0 text-sm text-fg-muted">
          Check reads nothing and changes nothing.
        </p>

        <!--
          The server refused the statement rather than the guard: a name that
          does not exist, or a string somebody wrote with double quotes and
          PostgreSQL read as a column. Its own sentence is the useful one.
        -->
        <div v-else-if="composer.refusal.value" :class="[reasons, refused]">
          <p>{{ composer.refusal.value.message }}</p>
        </div>

        <template v-else-if="composer.verdict.value">
          <div v-if="composer.allowed.value" class="flex items-baseline gap-3">
            <strong>The guard accepts this.</strong>
            <span v-if="composer.verdict.value.targetTable" class="font-mono">{{
              composer.verdict.value.targetTable
            }}</span>
            <span v-if="composer.verdict.value.estimatedRows !== undefined" class="text-fg-muted">
              about {{ composer.verdict.value.estimatedRows }} row(s)
            </span>
          </div>
          <ul v-else :class="reasons">
            <li
              v-for="(reason, index) in composer.verdict.value.reasons"
              :key="index"
              :class="reasonRow"
            >
              <code>{{ reason.rule }}</code>
              <span>{{ reason.message }}</span>
              <strong v-if="reason.subject" class="font-mono">{{ reason.subject }}</strong>
            </li>
          </ul>

          <!--
            What gets kept, before it is kept. "The rows are kept automatically"
            is a claim, and somebody about to ask for an approval should be able
            to read it as facts: how many rows, where they go, and the statement
            that would put them back.
          -->
          <section v-if="composer.verdict.value.backup" class="backup border-t border-line pt-3">
            <h3 class="m-0 mb-2 text-md">What would be kept</h3>
            <p class="m-0 mb-2 max-w-4xl">
              <strong>{{ composer.verdict.value.backup.rows }}</strong>
              row(s) of
              <strong class="font-mono">{{ composer.verdict.value.targetTable }}</strong>
              <template v-if="composer.verdict.value.backup.direction === 'before'">
                as they are now,
              </template>
              <template v-else> as this adds them, </template>
              into
              <code>{{ composer.verdict.value.backup.table }}</code>
              in
              <strong>{{ database }}</strong
              >, in the same transaction as the change.
            </p>
            <p class="m-0 mb-2 max-w-4xl text-fg-soft">
              And this is what would put them back. The zeros are where this request's own id goes
              once it exists; everything else is what will be stored.
            </p>
            <VSql :code="composer.verdict.value.backup.reverseStatement" size="sm" />
          </section>
        </template>

        <VField label="Why" for="write-reason" wide>
          <VInput
            id="write-reason"
            v-model="composer.reason.value"
            type="text"
            autocomplete="off"
            placeholder="What this is fixing, and roughly how many rows you expect."
          />
        </VField>

        <div v-if="requests.refusal.value" :class="[reasons, refused]">
          <p>{{ requests.refusal.value.message }}</p>
          <ul>
            <li
              v-for="(reason, index) in requests.refusal.value.reasons"
              :key="index"
              :class="reasonRow"
            >
              <code>{{ reason.rule }}</code>
              <span>{{ reason.message }}</span>
            </li>
          </ul>
        </div>
      </section>
    </main>
  </div>
</template>
