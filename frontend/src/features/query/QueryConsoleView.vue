<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import GuardChip from './GuardChip.vue'
import { readRules } from './guardRules'
import SqlEditor from './SqlEditor.vue'
import { maxTabs, useEditorTabs } from './useEditorTabs'
import { useQueryConsole } from './useQueryConsole'
import {
  VBanner,
  VButton,
  VConnectionBar,
  VEditorTab,
  VIcon,
  VInput,
  VLoader,
  VReadout,
  VResultPane,
  VToolbar,
  type ReadoutPart,
  type ResultTab,
} from '@/components'
import { useTargetStore } from '@/stores/target'
import { useRouteTarget } from '@/features/search/useRouteTarget'
import SchemaTree from '@/features/schema/SchemaTree.vue'
import SchemaChartPanel from '@/features/schema/SchemaChartPanel.vue'
import { useSchemaBrowser } from '@/features/schema/useSchemaBrowser'
import { describeFailure, describeReason, type TableRef } from '@/shared/api'
import { lazy } from '@/shared/lazy'
import { label as vimLabel, useVimMode } from '@/shared/vimMode'
import { useSessionStore } from '@/stores/session'

const defaultTop = 100

/*
 * What only a statement that ran needs is fetched when one has: the console's
 * first load carries the editor and nothing it cannot use yet (ADR 0009 P2).
 */
const ResultGrid = lazy(() => import('./ResultGrid.vue'), 'Loading the rows')
const GuardVerdictPanel = lazy(() => import('./GuardVerdictPanel.vue'), 'Loading the plan')

const session = useSessionStore()

/**
 * The deployment names itself, and the server it governs, on the current user.
 * requireSession has loaded it before this view mounts.
 *
 * Both were constants here until FEAT-012's groundwork: the id duplicated one
 * the backend already derives from the target DSN, and the name was the local
 * stack's, displayed unchanged on every deployment that copied it.
 *
 * The environment falls back to production, because the two ways to be wrong
 * are not equal: a banner that warns on a deployment where it need not costs an
 * operator nothing, and one that stays quiet on production costs them the
 * statement they were about to run. The instance has no such safe guess, and
 * an empty id is the better failure -- the schema request refuses it, where a
 * guessed one would read somewhere nobody chose.
 */
const environment = computed(() => session.caller?.environment ?? 'production')
const instanceName = computed(() => session.caller?.instance.name ?? '')

// One instance for now. FEAT-012 turns this into a picker fed by db_instance.
const instanceId = session.caller?.instance.id ?? ''

const vim = useVimMode()
const browser = useSchemaBrowser(instanceId)
const panel = useQueryConsole()
/*
 * FEAT-065 AC-5: up to eight statements, each with its own database, kept in
 * this browser. `statement` is always the open tab's, so everything below reads
 * one statement as it always did.
 */
const editorTabs = useEditorTabs(session.caller?.username ?? '')
const statement = ref(editorTabs.active.value.statement)
const top = ref(defaultTop)

const tableLabel = computed(() => {
  const table = browser.table.value
  return table === null ? null : `${table.schema}.${table.name}`
})

const target = useTargetStore()
watch([browser.database, tableLabel], ([database, table]) => target.aim(database, table), {
  immediate: true,
})

/*
 * The result pane: rows, the guard's reading of the plan, and whatever went
 * wrong, one at a time. It turns to whichever the last statement produced, so
 * a refusal is never left behind an empty grid.
 */
const pane = ref<ResultTab>('result')

/*
 * What the last statement did, as the pane's readout: the verdict first, then
 * the figures an operator quotes back — rows, time, the audit id, and how many
 * columns came back masked.
 */
const readout = computed((): ReadoutPart[] => {
  const result = panel.result.value
  if (result !== null) {
    const masked = result.columns.filter((column) => column.masked === true).length
    return [
      { value: 'ALLOWED', tone: 'ok' },
      { key: 'ROWS', value: String(result.rowCount) },
      { key: 'ELAPSED', value: `${result.elapsedMs} MS` },
      { key: 'LOG', value: result.auditId, tone: 'primary' },
      { key: 'MASKED', value: `${masked} COL` },
    ]
  }
  const verdict = panel.verdict.value
  if (verdict === null) return []
  if (verdict.decision === 'allow') return [{ value: 'ALLOWED', tone: 'ok' }]
  return [
    { value: 'REFUSED', tone: 'danger' },
    ...verdict.reasons.map((reason) => ({ value: reason.rule })),
  ]
})

/** What the pane is waiting for, so the tab stays put and says so instead of emptying. */
const runningLabel = computed(() => {
  const running = panel.running.value
  if (running === null) return null
  return running === 'execute' ? 'Running the statement' : 'Inspecting the statement'
})

const messageCount = computed(() => {
  const reasons = panel.verdict.value?.decision === 'deny' ? panel.verdict.value.reasons.length : 0
  return reasons + (bannerText.value === null ? 0 : 1)
})

watch(statement, (text) => editorTabs.update({ statement: text }))
watch(browser.database, (database) => editorTabs.update({ database }))

function showActive(): void {
  statement.value = editorTabs.active.value.statement
  void browser.chooseDatabase(editorTabs.active.value.database)
}

function switchTab(id: string): void {
  if (id === editorTabs.activeId.value) return
  editorTabs.select(id)
  showActive()
}

function openTab(): void {
  if (editorTabs.open()) showActive()
}

function closeTab(id: string): void {
  const wasOpen = id === editorTabs.activeId.value
  editorTabs.close(id)
  if (wasOpen) showActive()
}

const canRun = computed(
  () =>
    browser.database.value !== null &&
    statement.value.trim() !== '' &&
    !panel.busy.value &&
    !browser.busy.value,
)

// A guard rejection is rendered rule by rule below the editor. Repeating it in
// the banner would show the operator the same sentence twice.
const bannerText = computed(() => {
  const failure = panel.failure.value ?? browser.failure.value
  if (failure === null || failure === undefined || failure.kind === 'rejected') return null
  return describeFailure(failure)
})

watch(
  () => [panel.result.value, panel.verdict.value, bannerText.value] as const,
  ([result, verdict, failure]) => {
    if (result !== null) pane.value = 'result'
    else if (verdict !== null) pane.value = 'plan'
    else if (failure !== null) pane.value = 'messages'
  },
)

function request() {
  return {
    instanceId,
    database: browser.database.value ?? '',
    statement: statement.value,
  }
}

async function insert(): Promise<void> {
  const built = await browser.buildStatement(top.value)
  if (built !== null) statement.value = built
}

/**
 * Picking a table writes the statement for it.
 *
 * Every readable column starts ticked, so the statement an operator wanted is
 * the one the tick boxes already describe — and making them press Insert to get
 * it was a step between them and the thing they came here to do. Insert stays,
 * for after they change the ticks.
 */
async function chooseTable(next: TableRef | null): Promise<void> {
  await browser.chooseTable(next)
  if (next !== null) await insert()
}

useRouteTarget(browser, chooseTable)

const PlanCostGauge = defineAsyncComponent(() => import('./PlanCostGauge.vue'))

/**
 * Choosing an ordering rewrites the statement, the same way ticking a column
 * and pressing Insert does. Leaving it to the operator to press Insert again
 * would make the control look like it had not worked.
 */
async function orderBy(column: string): Promise<void> {
  browser.orderBy.value = column
  await insert()
}

async function toggleDirection(): Promise<void> {
  browser.descending.value = !browser.descending.value
  await insert()
}

/**
 * What the editor completes from: the table in hand and its readable columns.
 *
 * Denied columns are left out. Offering one would be suggesting a statement the
 * guard refuses, which teaches an operator to distrust the suggestions.
 */
const catalogue = computed(() => {
  const table = browser.table.value
  if (table === null) return {}
  const readable = browser.columns.value
    .filter((column) => !column.denied)
    .map((column) => column.name)
  // Nested, because that is the shape lang-sql resolves: a flat "public.member"
  // key is a table whose name contains a dot, not a table in a schema.
  return { [table.schema]: { [table.name]: readable } }
})

const defaultTable = computed(() => browser.table.value?.name ?? '')
const defaultSchema = computed(() => browser.table.value?.schema ?? '')

function execute(): void {
  if (canRun.value) void panel.execute(request())
}

function inspect(): void {
  if (canRun.value) void panel.inspect(request())
}

/**
 * Cmd/Ctrl+Enter runs, Cmd/Ctrl+E inspects.
 *
 * Bound on the window rather than the editor so they work with focus anywhere on
 * the page: an operator who has just ticked a column should not have to click
 * into the editor to run. The shortcuts are printed on the buttons, which is the
 * only reason anyone will find them.
 */
function onKeydown(event: KeyboardEvent): void {
  if (!(event.metaKey || event.ctrlKey)) return
  if (event.key === 'Enter') {
    event.preventDefault()
    execute()
    return
  }
  if (event.key.toLowerCase() === 'e') {
    event.preventDefault()
    inspect()
  }
}

onMounted(() => {
  void browser.loadDatabases().then(() => {
    if (browser.failure.value !== null) return
    editorTabs.keepReachable(browser.databases.value)
    statement.value = editorTabs.active.value.statement
    const database = editorTabs.active.value.database
    if (database !== null) void browser.chooseDatabase(database)
  })
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div
    class="workspace grid min-h-0 flex-1 grid-cols-[var(--spacing-tree)_minmax(0,1fr)] overflow-hidden"
  >
    <SchemaTree
      :databases="browser.databases.value"
      :tables="browser.tables.value"
      :columns="browser.columns.value"
      :database="browser.database.value"
      :table="browser.table.value"
      :selected="browser.selected.value"
      :all-selected="browser.allSelected.value"
      :busy="browser.busy.value"
      :order-by="browser.orderBy.value"
      :descending="browser.descending.value"
      :orderable-columns="browser.orderableColumns.value"
      :temporal-columns="browser.temporalColumns.value"
      @choose-database="browser.chooseDatabase"
      @choose-table="chooseTable"
      @order-by="orderBy"
      @toggle-direction="toggleDirection"
      @toggle="browser.toggle"
      @toggle-all="browser.toggleAll"
      @insert="insert"
    >
      <template #chart>
        <SchemaChartPanel
          :instance-id="instanceId"
          :database="browser.database.value"
          :target="browser.table.value"
          @choose="chooseTable"
        />
      </template>
    </SchemaTree>

    <main class="editor-pane flex min-h-0 min-w-0 flex-col overflow-hidden bg-surface">
      <h1 class="sr-only">Query</h1>
      <VToolbar :busy="panel.busy.value" busy-label="running">
        <template #lead>
          <VButton variant="primary" :disabled="!canRun" @click="execute">
            <VIcon name="play" />
            Execute
            <span class="kbd">⌘↵</span>
          </VButton>
          <VButton :disabled="!canRun" @click="inspect">
            <VIcon name="scan" />
            Inspect
            <span class="kbd">⌘E</span>
          </VButton>
          <GuardChip :rules="readRules" />
        </template>

        <label class="flex items-center gap-1.5">
          <span class="hud text-fg-muted">Limit</span>
          <span class="flex w-20 font-mono">
            <VInput v-model.number="top" size="sm" type="number" min="1" max="100000" />
          </span>
        </label>
      </VToolbar>

      <div
        class="tabs flex h-10 flex-none items-end gap-2 border-b border-line bg-surface-sunken px-2"
      >
        <div class="flex min-w-0 items-end gap-px" role="tablist" aria-label="Open statements">
          <VEditorTab
            v-for="tab in editorTabs.tabs.value"
            :key="tab.id"
            :label="tab.database ?? 'Untitled'"
            :environment="environment"
            :active="tab.id === editorTabs.activeId.value"
            @select="switchTab(tab.id)"
            @close="closeTab(tab.id)"
          />
        </div>
        <button
          type="button"
          class="new-tab mb-1 grid size-7 flex-none cursor-pointer place-items-center rounded-control border-0 bg-transparent p-0 text-fg-muted hover:bg-surface hover:text-fg"
          :aria-label="`Open another statement (at most ${maxTabs})`"
          @click="openTab"
        >
          <VIcon name="plus" size="sm" />
        </button>
        <p v-if="editorTabs.refusal.value" class="refusal m-0 mb-2 text-xs text-warn" role="status">
          {{ editorTabs.refusal.value }}
        </p>
        <div class="ml-auto flex min-w-0 self-center">
          <VConnectionBar
            :instance="instanceName"
            :environment="environment"
            :database="browser.database.value"
            :table="tableLabel"
            :databases="browser.databases.value"
            @choose="browser.chooseDatabase"
          />
        </div>
      </div>

      <div class="editor-holder flex-none bg-surface">
        <SqlEditor
          v-model="statement"
          :schema="catalogue"
          :default-table="defaultTable"
          :default-schema="defaultSchema"
          :vim-enabled="vim.enabled.value"
          @update:vim-mode="vim.mode.value = $event"
        />
        <!-- Outside the editor on purpose: somebody who turned Vim on by
             accident cannot type, and the way out must not need Vim (AC-5). -->
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
            :aria-pressed="vim.enabled.value"
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

      <VResultPane v-model="pane" :messages="messageCount">
        <template #readout>
          <VReadout
            v-if="readout.length > 0"
            :parts="readout"
            label="What the last statement did"
          />
        </template>
        <template #result>
          <div v-if="runningLabel" class="flex justify-center py-10">
            <VLoader size="section" :label="runningLabel" />
          </div>
          <ResultGrid v-else-if="panel.result.value" :result="panel.result.value" />
          <p v-else class="m-0 px-4 py-10 text-center text-sm text-fg-muted">
            Rows appear here when a statement runs.
          </p>
        </template>
        <template #gauge>
          <PlanCostGauge
            :verdict="panel.verdict.value"
            :result="panel.result.value"
            :statement="statement"
          />
        </template>
        <template #plan>
          <div v-if="runningLabel" class="flex justify-center py-10">
            <VLoader size="section" :label="runningLabel" />
          </div>
          <GuardVerdictPanel v-else-if="panel.verdict.value" :verdict="panel.verdict.value" />
          <p v-else class="m-0 px-4 py-10 text-center text-sm text-fg-muted">
            Inspect a statement to read its plan.
          </p>
        </template>
        <template #messages>
          <div v-if="runningLabel" class="flex justify-center py-10">
            <VLoader size="section" :label="runningLabel" />
          </div>
          <VBanner v-if="bannerText" icon="circle-x">{{ bannerText }}</VBanner>
          <ul
            v-if="panel.verdict.value?.decision === 'deny'"
            class="messages m-0 flex list-none flex-col gap-2 p-4"
          >
            <li
              v-for="(reason, index) in panel.verdict.value.reasons"
              :key="index"
              class="flex items-baseline gap-2 text-sm"
            >
              <span
                class="rule flex-none rounded-control border border-danger-line bg-danger-bg px-1.25 font-mono text-xs text-danger"
                >{{ reason.rule }}</span
              >
              <span>{{ describeReason(reason) }}</span>
            </li>
          </ul>
          <p
            v-if="messageCount === 0 && !runningLabel"
            class="m-0 px-4 py-10 text-center text-sm text-fg-muted"
          >
            No messages.
          </p>
        </template>
      </VResultPane>
    </main>
  </div>
</template>
