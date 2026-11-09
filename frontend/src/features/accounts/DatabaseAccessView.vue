<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { grantableRoles, useDatabaseGrants } from './useDatabaseGrants'
import { VBadge, VBanner, VButton, VEmpty, VPageHeader, VSkeleton, VTable } from '@/components'
import { describeFailure } from '@/shared/api'
import { roleLabel } from '@/shared/roles'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

const page = useDatabaseGrants()
const session = useSessionStore()

const bannerText = computed(() => {
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

// FEAT-058 AC-8. The matrix is readable by anybody signed in — knowing which
// database your role does not reach is what stops a refusal reading as an
// outage — and changing it needs permission.manage.
const mayChange = computed(() => session.caller?.permissions.includes('permission.manage') === true)

const when = (value: string): string => stamp(value)

function toggle(database: string, role: string): void {
  if (!mayChange.value) return
  void page.set(database, role, !page.granted(database, role))
}

const skeletonHeadings = ['Database', ...grantableRoles.map((role) => roleLabel(role))]
const skeletonColumns = ['w-21/25', ...grantableRoles.map(() => 'w-1/2')]

onMounted(() => void page.load())
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="key"
      title="Database access"
      :count="page.rows.value.length"
      :subtitle="mayChange ? '' : 'Read-only: changing this needs permission.manage'"
      :busy="page.busy.value"
    />

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

    <div class="min-h-0 flex-1 overflow-auto">
      <VTable v-if="page.rows.value.length > 0">
        <template #caption>Governed databases and the roles that reach them</template>
        <template #head>
          <th scope="col">Database</th>
          <th v-for="role in grantableRoles" :key="role" scope="col">
            {{ roleLabel(role) }}
          </th>
        </template>

        <tr v-for="database in page.rows.value" :key="database">
          <td class="w-1/100 font-mono whitespace-nowrap">{{ database }}</td>
          <td v-for="role in grantableRoles" :key="role" class="w-1/100 whitespace-nowrap">
            <VButton
              size="sm"
              :variant="page.granted(database, role) ? 'primary' : 'ghost'"
              :icon="page.granted(database, role) ? 'check' : 'minus'"
              :disabled="!mayChange || page.changing.value === database + '/' + role"
              :title="
                page.granted(database, role)
                  ? `${roleLabel(role)} reaches ${database}. Click to take it away.`
                  : `${roleLabel(role)} does not reach ${database}.`
              "
              @click="toggle(database, role)"
            >
              {{ page.granted(database, role) ? 'Reaches' : 'Grant' }}
            </VButton>
          </td>
        </tr>
      </VTable>

      <VSkeleton
        v-else-if="page.busy.value"
        variant="table"
        controls
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading database access"
      />
      <VEmpty v-else>
        No governed databases. Avior reads them from the server it was pointed at.
      </VEmpty>

      <!-- FEAT-058 AC-7. The rows existed before this and no page showed them,
           which made the record worth nothing: a grant is access to a
           production database, and who gave it has to be readable. -->
      <section v-if="page.history.value.length > 0" class="history border-t border-line px-3 py-4">
        <h2 class="mt-0 mb-2 text-sm font-semibold">What changed</h2>
        <VTable variant="record">
          <template #head>
            <th scope="col">What</th>
            <th scope="col">Database</th>
            <th scope="col">Role</th>
            <th scope="col">By</th>
            <th scope="col">When</th>
            <th scope="col">Why</th>
          </template>

          <tr v-for="(change, index) in page.history.value" :key="index">
            <td>
              <VBadge :tone="change.action === 'grant' ? 'ok' : 'danger'">
                {{ change.action }}
              </VBadge>
            </td>
            <td class="font-mono">{{ change.database }}</td>
            <td>{{ roleLabel(change.role) }}</td>
            <td>{{ change.actor === '' ? 'the deployment' : change.actor }}</td>
            <td class="tabular-nums" :title="fullStamp(change.changedAt)">
              {{ when(change.changedAt) }}
            </td>
            <td class="max-w-md whitespace-normal text-fg-soft italic">{{ change.reason }}</td>
          </tr>
        </VTable>
      </section>
    </div>
  </div>
</template>
