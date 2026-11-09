<script setup lang="ts">
/**
 * FEAT-076 AC-18. One role, in full, in a dialog a card on the roles map opens.
 *
 * Who it carries or belongs to, the other databases it may connect to, and
 * what it may do in this one as a matrix — a row per table or routine, a
 * column per privilege, a marker where the privilege comes through somebody
 * else and a footnote saying who. The owner gets one line instead: it owns
 * every table and so holds every privilege.
 */
import { computed } from 'vue'

import { VBadge, VBanner, VButton, VDialog, VSkeleton, VTable } from '@/components'
import { describeFailure, type Failure } from '@/shared/api'
import {
  ownership,
  routineMatrix,
  superscript,
  tableMatrix,
  type Cell,
  type Matrix,
} from './roleMatrix'
import type { ProvisionedRole, RoleAccess } from './useRoles'

const props = defineProps<{
  role: ProvisionedRole | null
  access: RoleAccess | null
  failure: Failure | null
  reading: boolean
  busy: boolean
}>()
const emit = defineEmits<{
  close: []
  rotate: [name: string]
  drop: [name: string]
  grantConnect: [name: string]
}>()

const name = computed(() => props.role?.name ?? '')

/** The databases besides this one, each marked where PUBLIC is why. */
const elsewhere = computed(
  () => props.access?.connectable.filter((db) => db.database !== props.access?.database) ?? [],
)

const tables = computed<Matrix | null>(() =>
  props.access === null || props.role?.kind === 'owner' ? null : tableMatrix(props.access),
)
const heldRows = computed(
  () =>
    tables.value?.rows.filter((row) =>
      Object.values(row.cells).some((cell) => cell.held !== 'no'),
    ) ?? [],
)
const quietRows = computed(() => (tables.value?.rows.length ?? 0) - heldRows.value.length)
const routines = computed(() => (props.access === null ? null : routineMatrix(props.access)))
const owns = computed(() => (props.access === null ? null : ownership(props.access)))

/** What a cell says: held, held on some columns, or not held. */
function glyph(cell: Cell | undefined): string {
  if (cell?.held === 'yes') return '✓'
  if (cell?.held === 'columns') return 'cols'
  return '—'
}

/** The superscript pointing at the footnote that says where it comes from. */
function marker(cell: Cell | undefined): string {
  if (cell === undefined || cell.held === 'no' || cell.marker === null) return ''
  return superscript(cell.marker)
}

function columnsTitle(cell: Cell | undefined): string {
  return cell?.held === 'columns' ? `Only on ${cell.columns.join(', ')}` : ''
}

const section = 'mt-5 mb-2 eyebrow text-2xs text-fg-muted'
const line = 'm-0 text-sm'
const code = 'font-mono text-sm'
</script>

<template>
  <VDialog :open="role !== null" :title="name" motion="fade" @close="emit('close')">
    <template #badge>
      <VBadge v-if="role" size="sm">{{ role.kind }}</VBadge>
    </template>
    <template #status>
      <template v-if="access">
        <VBadge v-if="access.canConnect" tone="ok" data-testid="dialog-status">✓ connect</VBadge>
        <VBadge v-else tone="warn" data-testid="dialog-status">⚠ cannot connect</VBadge>
      </template>
    </template>

    <VBanner v-if="failure" tone="danger">{{ describeFailure(failure) }}</VBanner>
    <VSkeleton v-if="reading && access === null" :lines="6" label="Reading what the role may do" />

    <div v-if="access && role" class="flex flex-col" data-testid="role-dialog-body">
      <p v-if="role.kind === 'group'" :class="line" data-testid="dialog-members">
        <span class="text-fg-muted">Members:</span>
        {{ role.members.length > 0 ? role.members.join(', ') : 'no login yet' }}
      </p>
      <p v-else-if="role.kind === 'login'" :class="line" data-testid="dialog-member-of">
        <span class="text-fg-muted">Member of:</span>
        {{ role.memberOf.length > 0 ? role.memberOf.join(', ') : 'no group' }}
      </p>
      <p :class="line" class="mt-1" data-testid="dialog-elsewhere">
        <span class="text-fg-muted">Other databases:</span>
        <template v-if="elsewhere.length === 0"> none</template>
        <template v-for="(db, index) in elsewhere" :key="db.database">
          {{ index === 0 ? ' ' : ' · ' }}<code :class="code">{{ db.database }}</code
          ><span v-if="db.via.includes('PUBLIC')" class="text-fg-muted"> (via PUBLIC)</span>
        </template>
      </p>
      <p v-if="!access.canConnect" :class="line" class="mt-1 text-warn">
        Cannot connect to <code :class="code">{{ access.database }}</code
        >. Grant CONNECT to this role, or to a group it belongs to.
      </p>

      <template v-if="role.kind === 'owner' && owns">
        <h3 :class="section">Tables in {{ access.database }}</h3>
        <p :class="line" data-testid="dialog-owns">
          Owns all {{ owns.count }} {{ owns.count === 1 ? 'table' : 'tables' }} — every privilege
        </p>
        <p v-if="owns.tables && owns.tables.length > 0" :class="line" class="mt-1 text-fg-muted">
          {{ owns.tables.join(', ') }}
        </p>
      </template>

      <template v-else-if="tables">
        <h3 :class="section">Tables in {{ access.database }}</h3>
        <p v-if="heldRows.length === 0" :class="line" class="text-fg-muted">
          No privilege on any of its {{ tables.rows.length }} tables and views.
        </p>
        <VTable v-else variant="list" data-testid="dialog-tables">
          <template #caption>Privileges on the tables of {{ access.database }}</template>
          <template #head>
            <th scope="col">Table</th>
            <th
              v-for="privilege in tables.privileges"
              :key="privilege"
              scope="col"
              class="text-center"
            >
              {{ privilege }}
            </th>
          </template>
          <tr v-for="row in heldRows" :key="row.name" :data-table="row.name">
            <th scope="row" class="text-left font-normal">
              <code :class="code">{{ row.name }}</code>
            </th>
            <td
              v-for="privilege in tables.privileges"
              :key="privilege"
              class="text-center"
              :data-privilege="privilege"
              :title="columnsTitle(row.cells[privilege])"
            >
              <span :class="glyph(row.cells[privilege]) === '—' ? 'text-fg-muted' : ''">{{
                glyph(row.cells[privilege])
              }}</span
              ><sup v-if="marker(row.cells[privilege])">{{ marker(row.cells[privilege]) }}</sup>
            </td>
          </tr>
        </VTable>
        <p v-if="quietRows > 0 && heldRows.length > 0" :class="line" class="mt-2 text-fg-muted">
          Nothing on {{ quietRows }} more.
        </p>
        <ul
          v-if="tables.footnotes.length > 0"
          class="mt-2 mb-0 list-none p-0 text-2xs text-fg-muted"
          data-testid="dialog-footnotes"
        >
          <li v-for="note in tables.footnotes" :key="note.marker">
            {{ superscript(note.marker) }} {{ note.text }}
          </li>
        </ul>
      </template>

      <template v-if="routines">
        <h3 :class="section">Routines</h3>
        <p v-if="routines.rows.length === 0" :class="line" class="text-fg-muted">
          No functions or procedures outside the system schemas.
        </p>
        <VTable v-else variant="list" data-testid="dialog-routines">
          <template #caption>EXECUTE on routines</template>
          <template #head>
            <th scope="col">Routine</th>
            <th scope="col" class="text-center">EXECUTE</th>
          </template>
          <tr v-for="row in routines.rows" :key="row.name" :data-routine="row.name">
            <th scope="row" class="text-left font-normal">
              <code :class="code">{{ row.name }}</code>
            </th>
            <td class="text-center">
              <span :class="glyph(row.cells.EXECUTE) === '—' ? 'text-fg-muted' : ''">{{
                glyph(row.cells.EXECUTE)
              }}</span
              ><sup v-if="marker(row.cells.EXECUTE)">{{ marker(row.cells.EXECUTE) }}</sup>
            </td>
          </tr>
        </VTable>
        <ul
          v-if="routines.footnotes.length > 0"
          class="mt-2 mb-0 list-none p-0 text-2xs text-fg-muted"
        >
          <li v-for="note in routines.footnotes" :key="note.marker">
            {{ superscript(note.marker) }} {{ note.text }}
          </li>
        </ul>
      </template>

      <p v-if="access.truncated" :class="line" class="mt-2 text-warn">
        This database has more than 500 tables or routines; the first 500 of each are shown.
      </p>
    </div>

    <template v-if="role && role.kind !== 'owner'" #footer>
      <VButton
        v-if="role.kind === 'group' && access && !access.canConnect"
        variant="primary"
        :disabled="busy"
        @click="emit('grantConnect', role.name)"
      >
        Grant connect
      </VButton>
      <VButton v-if="role.kind === 'login'" :disabled="busy" @click="emit('rotate', role.name)">
        New password
      </VButton>
      <VButton variant="bare" :disabled="busy" @click="emit('drop', role.name)">Drop</VButton>
    </template>
  </VDialog>
</template>
