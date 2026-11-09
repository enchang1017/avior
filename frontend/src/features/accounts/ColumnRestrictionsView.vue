<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { key, unlockableRoles, useColumnPolicy, type ColumnRestriction } from './useColumnPolicy'
import {
  VBadge,
  VBanner,
  VButton,
  VDateTime,
  VDialog,
  VEmpty,
  VField,
  VInput,
  VPageHeader,
  VSelect,
  VSkeleton,
  VTable,
  type Tone,
} from '@/components'
import { describeFailure } from '@/shared/api'
import { roleLabel } from '@/shared/roles'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

const page = useColumnPolicy()
const session = useSessionStore()

const bannerText = computed(() => {
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

// FEAT-055 AC-7, AC-8. Anybody signed in reads which columns are restricted;
// changing it needs permission.manage, which the DBA and the administrator
// hold.
const mayChange = computed(() => session.caller?.permissions.includes('permission.manage') === true)

/*
 * Unlocking asks for a reason, so it is a dialog rather than a toggle.
 *
 * That is the point rather than an inconvenience: restricting is the careful
 * default and needs no defence, and unlocking is the exception somebody has to
 * be able to justify afterwards. A one-click unlock would leave the reason
 * empty on every row that mattered.
 */
const asking = ref<{ restriction: ColumnRestriction; role: string } | null>(null)
const reason = ref('')
const expiresAt = ref('')

/*
 * Adding a restriction: pick a database, then a table, then a column.
 *
 * Read through the administration's own endpoints rather than the schema
 * browser's, which answers what the reader's roles reach — an administrator
 * holding no database grant could not pick the column they were asked to
 * restrict, and this page would only ever manage what somebody had already put
 * in the table by hand.
 */
const adding = ref(false)
const pick = ref({ database: '', table: '', column: '', reason: '' })

const tableOptions = computed(() =>
  page.pickTables.value.map((table) => ({
    value: table.schema + '.' + table.name,
    label: table.schema + '.' + table.name,
  })),
)

const columnOptions = computed(() =>
  page.pickColumns.value.map((name) => ({ value: name, label: name })),
)

const databaseOptions = computed(() =>
  page.pickDatabases.value.map((name) => ({ value: name, label: name })),
)

function openAdd(): void {
  pick.value = { database: '', table: '', column: '', reason: '' }
  adding.value = true
}

async function chooseDatabase(database: string): Promise<void> {
  pick.value.database = database
  pick.value.table = ''
  pick.value.column = ''
  await page.loadTables(database)
}

async function chooseTable(qualified: string): Promise<void> {
  pick.value.table = qualified
  pick.value.column = ''
  const [schemaName, table] = qualified.split('.')
  await page.loadColumns(pick.value.database, schemaName ?? '', table ?? '')
}

const canAdd = computed(
  () => pick.value.database !== '' && pick.value.table !== '' && pick.value.column !== '',
)

async function addRestriction(): Promise<void> {
  const done = await page.setRestricted(
    pick.value.database,
    pick.value.table,
    pick.value.column,
    true,
    pick.value.reason,
  )
  if (done) adding.value = false
}

// Removing one is not the careful decision and needs no reason, but it is not
// undoable by a stray click either: it opens the column to everybody who
// reaches the database.
const removing = ref<ColumnRestriction | null>(null)

async function confirmRemove(): Promise<void> {
  const target = removing.value
  if (target === null) return
  const done = await page.setRestricted(target.database, target.object, target.column, false, '')
  if (done) removing.value = null
}

function ask(restriction: ColumnRestriction, role: string): void {
  if (!mayChange.value) return

  // Closing needs no reason: taking access away is not the thing anybody has
  // to justify.
  if (page.reads(restriction, role)) {
    void page.setUnlocked(restriction, role, false, '', '')
    return
  }
  asking.value = { restriction, role }
  reason.value = ''
  expiresAt.value = ''
}

async function confirm(): Promise<void> {
  const pending = asking.value
  if (pending === null) return

  const done = await page.setUnlocked(
    pending.restriction,
    pending.role,
    true,
    reason.value,
    expiresAt.value,
  )
  // Left open on a refusal, with the banner saying why, so whatever was typed
  // is still there to correct.
  if (done) asking.value = null
}

/*
 * Opening access reads as the notable one, closing it as ordinary.
 *
 * Restricting a column is the careful default, so it is not coloured as an
 * alarm; unlocking is the exception somebody chose to make.
 */
const actionTones: Record<string, Tone> = {
  unlock: 'warn',
  lock: 'neutral',
  restrict: 'neutral',
  unrestrict: 'warn',
}

function toneOf(action: string): Tone {
  return actionTones[action] ?? 'neutral'
}

const at = (value: string): string => stamp(value)

/**
 * What the control is about, and what already happened to it.
 *
 * An expired unlock is the interesting case: the role does not read the column
 * and somebody did allow it once. The title is where that fits without another
 * column of table.
 */
/** An unlock that has run out. The role does not read the column, and somebody
 * did allow it once. */
function wasUnlocked(restriction: ColumnRestriction, role: string): boolean {
  return page.unlockFor(restriction, role)?.expired === true
}

function unlockTitle(restriction: ColumnRestriction, role: string): string {
  const unlock = page.unlockFor(restriction, role)
  if (unlock === undefined) {
    return `${roleLabel(role)} cannot read ${restriction.column}.`
  }
  if (unlock.expired) {
    return `Unlocked by ${unlock.grantedBy || 'the deployment'} and expired${
      unlock.expiresAt ? ' on ' + at(unlock.expiresAt) : ''
    }: ${unlock.reason}`
  }
  return `Unlocked by ${unlock.grantedBy || 'the deployment'}${
    unlock.expiresAt ? ' until ' + at(unlock.expiresAt) : ''
  }: ${unlock.reason}`
}

const skeletonHeadings = [
  'Column',
  ...unlockableRoles.map((role) => roleLabel(role)),
  'Why it is restricted',
  '',
]
const skeletonColumns = ['w-21/25', ...unlockableRoles.map(() => 'w-1/2'), 'w-full', 'w-1/3']

onMounted(() => void page.load())
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="lock"
      title="Column restrictions"
      :count="page.restrictions.value.length"
      :subtitle="mayChange ? '' : 'Read-only: changing this needs permission.manage'"
      :busy="page.busy.value"
    >
      <VButton v-if="mayChange" variant="primary" icon="plus" @click="openAdd()">
        Restrict a column
      </VButton>
    </VPageHeader>

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

    <div class="min-h-0 flex-1 overflow-auto">
      <VTable v-if="page.restrictions.value.length > 0">
        <template #caption>Restricted columns and the roles unlocked for them</template>
        <template #head>
          <th scope="col">Column</th>
          <th v-for="role in unlockableRoles" :key="role" scope="col">
            {{ roleLabel(role) }}
          </th>
          <th scope="col">Why it is restricted</th>
          <th scope="col"><span class="sr-only">Remove</span></th>
        </template>

        <tr v-for="restriction in page.restrictions.value" :key="key(restriction)">
          <td class="who w-1/100 whitespace-nowrap">
            <div class="flex flex-col gap-0.5">
              <span class="font-mono font-semibold">{{ restriction.column }}</span>
              <span class="font-mono text-fg-soft"
                >{{ restriction.database }} · {{ restriction.object }}</span
              >
            </div>
          </td>

          <td v-for="role in unlockableRoles" :key="role" class="w-1/100 whitespace-nowrap">
            <VButton
              size="sm"
              :variant="page.reads(restriction, role) ? 'primary' : 'ghost'"
              :icon="page.reads(restriction, role) ? 'eye' : 'lock'"
              :disabled="!mayChange || page.changing.value === key(restriction) + '/' + role"
              :title="unlockTitle(restriction, role)"
              @click="ask(restriction, role)"
            >
              {{ page.reads(restriction, role) ? 'Reads' : 'Unlock' }}
            </VButton>
            <!-- An unlock that has ended reads as "not unlocked" without
                   this, and somebody did allow it once — which is the part
                   worth seeing when the question is who could have read it. -->
            <VBadge v-if="wasUnlocked(restriction, role)" tone="warn" class="ml-1.5">
              expired
            </VBadge>
          </td>

          <td class="max-w-sm text-fg-soft">{{ restriction.reason }}</td>

          <td class="w-1/100 whitespace-nowrap">
            <VButton
              v-if="mayChange"
              size="sm"
              variant="bare"
              danger
              icon="x"
              title="Make this column ordinary again. It opens to everybody who reaches the database."
              @click="removing = restriction"
            >
              Remove
            </VButton>
          </td>
        </tr>
      </VTable>

      <VSkeleton
        v-else-if="page.busy.value"
        variant="table"
        :row-lines="2"
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading column restrictions"
      />
      <VEmpty v-else>
        No column is restricted. Every column of every governed database is readable by whoever
        reaches the database.
      </VEmpty>

      <!-- FEAT-055 AC-9. An unlock is somebody deciding that a role may read
           something marked as careful, and a record of that is worth nothing if
           nobody can read it. -->
      <section v-if="page.history.value.length > 0" class="history border-t border-line px-3 py-4">
        <h2 class="mt-0 mb-2 text-sm font-semibold">What changed</h2>
        <VTable variant="record">
          <template #head>
            <th scope="col">What</th>
            <th scope="col">Column</th>
            <th scope="col">Role</th>
            <th scope="col">By</th>
            <th scope="col">When</th>
            <th scope="col">Until</th>
            <th scope="col">Why</th>
          </template>

          <tr v-for="(change, index) in page.history.value" :key="index">
            <td>
              <VBadge :tone="toneOf(change.action)">{{ change.action }}</VBadge>
            </td>
            <td class="font-mono">
              {{ change.object }}.<strong>{{ change.column }}</strong>
              <span class="ml-2 text-2xs text-fg-soft">{{ change.database }}</span>
            </td>
            <td>{{ change.role === '' ? '—' : roleLabel(change.role) }}</td>
            <td>{{ change.actor === '' ? 'the deployment' : change.actor }}</td>
            <td class="tabular-nums" :title="fullStamp(change.changedAt)">
              {{ at(change.changedAt) }}
            </td>
            <td class="tabular-nums" :title="change.expiresAt ? fullStamp(change.expiresAt) : ''">
              {{ change.expiresAt ? at(change.expiresAt) : '—' }}
            </td>
            <td class="max-w-md whitespace-normal text-fg-soft italic">{{ change.reason }}</td>
          </tr>
        </VTable>
      </section>
    </div>

    <VDialog :open="adding" title="Restrict a column" @close="adding = false">
      <p class="mt-0 mb-3 max-w-lg text-sm">
        A restricted column is refused to every role until somebody unlocks it for one. Pick it from
        any governed database — this list is not filtered by what your own roles reach.
      </p>

      <VField label="Database" for="pick-database" wide>
        <VSelect
          id="pick-database"
          :model-value="pick.database"
          :options="databaseOptions"
          @update:model-value="chooseDatabase($event)"
        />
      </VField>

      <VField label="Table or view" for="pick-table" wide>
        <VSelect
          id="pick-table"
          :model-value="pick.table"
          :options="tableOptions"
          :disabled="pick.database === ''"
          @update:model-value="chooseTable($event)"
        />
      </VField>

      <VField label="Column" for="pick-column" wide>
        <VSelect
          id="pick-column"
          v-model="pick.column"
          :options="columnOptions"
          :disabled="pick.table === ''"
        />
      </VField>

      <VField label="Why" for="pick-reason" wide>
        <VInput
          id="pick-reason"
          v-model="pick.reason"
          type="text"
          placeholder="What is in it, and who should not see it"
        />
      </VField>

      <div class="mt-4 flex gap-2">
        <VButton variant="primary" :disabled="!canAdd" @click="addRestriction()">Restrict</VButton>
        <VButton variant="bare" @click="adding = false">Cancel</VButton>
      </div>
    </VDialog>

    <VDialog
      :open="removing !== null"
      :title="removing === null ? '' : `Remove the restriction on ${removing.column}`"
      @close="removing = null"
    >
      <p class="mt-0 mb-3 max-w-lg text-sm">
        <strong class="font-mono">{{ removing?.object }}.{{ removing?.column }}</strong> becomes an
        ordinary column, readable by everybody who reaches
        <strong class="font-mono">{{ removing?.database }}</strong
        >. The unlocks it has are kept, inert, so the record of who was given it stays.
      </p>

      <div class="mt-4 flex gap-2">
        <VButton variant="primary" danger @click="confirmRemove()">Remove it</VButton>
        <VButton variant="bare" @click="removing = null">Cancel</VButton>
      </div>
    </VDialog>

    <VDialog
      :open="asking !== null"
      :title="asking === null ? '' : `Unlock ${asking.restriction.column}`"
      @close="asking = null"
    >
      <p class="mt-0 mb-3 max-w-lg text-sm">
        This opens
        <strong class="font-mono"
          >{{ asking?.restriction.object }}.{{ asking?.restriction.column }}</strong
        >
        to everybody holding
        <strong>{{ roleLabel(asking?.role ?? '') }}</strong
        >. What you write here is what somebody reads when they ask why.
      </p>

      <VField label="Reason" for="unlock-reason" wide>
        <VInput
          id="unlock-reason"
          v-model="reason"
          type="text"
          placeholder="Who asked, and what for"
        />
      </VField>

      <VField label="Until" for="unlock-expires" wide>
        <VDateTime
          id="unlock-expires"
          v-model="expiresAt"
          placeholder="Until somebody removes it"
        />
      </VField>
      <p class="text-fg-soft">
        Leave the time empty and the unlock lasts until somebody removes it. A time makes it stop on
        its own, which is what temporary has to mean if it is to mean anything.
      </p>

      <div class="mt-4 flex gap-2">
        <VButton variant="primary" :disabled="reason.trim() === ''" @click="confirm()">
          Unlock
        </VButton>
        <VButton variant="bare" @click="asking = null">Cancel</VButton>
      </div>
    </VDialog>
  </div>
</template>
