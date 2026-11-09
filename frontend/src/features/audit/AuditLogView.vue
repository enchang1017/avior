<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'

import AuditSkyStrip from './AuditSkyStrip.vue'
import { useAuditLog, type AuditEntry, type AuditScope } from './useAuditLog'
import {
  type SelectOption,
  VBanner,
  VButton,
  VDateTime,
  VEmpty,
  VField,
  VFilterBar,
  VIcon,
  VInput,
  VPageHeader,
  VPager,
  VSelect,
  VSkeleton,
  VTable,
} from '@/components'
import { describeFailure, describeReason } from '@/shared/api'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

const verdicts: SelectOption[] = [
  { value: '', label: 'Any' },
  { value: 'allow', label: 'Allowed' },
  { value: 'deny', label: 'Refused' },
]

const props = defineProps<{ scope: AuditScope }>()

const session = useSessionStore()
const audit = useAuditLog(() => props.scope)

/**
 * The switch to the administrator's log is offered on the permission the
 * endpoint itself enforces, not on a role: `audit.read.all` is what the server
 * checks, and gating the link on anything else would offer a page that answers
 * 403.
 */
const canReadAll = computed(() => session.caller?.permissions.includes('audit.read.all') === true)

const heading = computed(() =>
  props.scope === 'all' ? "Every operator's submissions" : 'Your submissions',
)

const bannerText = computed(() => {
  const failure = audit.failure.value
  return failure === null ? null : describeFailure(failure)
})

function submittedAt(entry: AuditEntry): string {
  return stamp(entry.createdAt)
}

/**
 * A statement that never ran has no row count and no elapsed time, and showing
 * either as 0 would claim it returned nothing rather than that it was never
 * asked. FEAT-003 AC-9.
 */
function ran(value: number | null | undefined): value is number {
  return value !== null && value !== undefined
}

const waiting = computed(
  () => audit.busy.value && audit.entries.value.length === 0 && audit.failure.value === null,
)
const skeletonHeadings = computed(() => [
  'Submitted',
  ...(props.scope === 'all' ? ['Operator'] : []),
  'Database',
  'Statement',
  'Verdict',
  'Rows',
  'Elapsed',
])
const skeletonColumns = computed(() => [
  'w-21/25',
  ...(props.scope === 'all' ? ['w-1/2'] : []),
  'w-21/25',
  'w-full',
  'w-1/2',
  'w-1/3',
  'w-1/3',
])

onMounted(() => {
  void audit.load()
})
</script>

<template>
  <div class="list-page">
    <VPageHeader icon="scroll" :title="heading" :busy="audit.busy.value">
      <VButton
        v-if="canReadAll"
        :icon="props.scope === 'all' ? 'user' : 'users'"
        :to="{ name: props.scope === 'all' ? 'audit-mine' : 'audit-all' }"
      >
        {{ props.scope === 'all' ? 'Your submissions' : "Everyone's submissions" }}
      </VButton>
    </VPageHeader>

    <VFilterBar :busy="audit.busy.value" icon="filter" @submit="audit.apply()">
      <VField label="From" for="audit-from">
        <VDateTime id="audit-from" v-model="audit.from.value" placeholder="Any time" />
      </VField>
      <VField label="To" for="audit-to">
        <VDateTime id="audit-to" v-model="audit.to.value" placeholder="Any time" />
      </VField>

      <template v-if="props.scope === 'all'">
        <VField label="Operator" for="audit-operator">
          <VInput
            id="audit-operator"
            v-model="audit.operator.value"
            type="search"
            placeholder="Any part of a username"
          />
        </VField>
        <VField label="Statement" for="audit-statement" wide>
          <VInput
            id="audit-statement"
            v-model="audit.statement.value"
            type="search"
            placeholder="Any part of the statement"
          />
        </VField>
        <VField label="Verdict" for="audit-decision">
          <VSelect id="audit-decision" v-model="audit.decision.value" :options="verdicts" />
        </VField>
      </template>
    </VFilterBar>

    <VBanner v-if="audit.forbidden.value" icon="lock">
      {{ bannerText }}
      <RouterLink
        :to="{ name: 'audit-mine' }"
        class="ml-auto font-semibold text-inherit"
        aria-label="Read your own submissions"
      >
        Read your own submissions
      </RouterLink>
    </VBanner>
    <VBanner v-else-if="bannerText" icon="circle-x">{{ bannerText }}</VBanner>

    <VEmpty v-else-if="audit.entries.value.length === 0 && !audit.busy.value">
      No submission in this range.
    </VEmpty>

    <div class="audit-strip flex-none">
      <AuditSkyStrip :scope="props.scope" :log="audit" />
    </div>

    <div v-if="audit.entries.value.length > 0" class="min-h-0 flex-1 overflow-auto">
      <VTable variant="log">
        <template #caption>{{ heading }}, newest first</template>
        <template #head>
          <th scope="col">Submitted</th>
          <th v-if="props.scope === 'all'" scope="col">Operator</th>
          <th scope="col">Database</th>
          <th scope="col" class="w-2/5">Statement</th>
          <th scope="col">Verdict</th>
          <th scope="col" class="text-right font-mono whitespace-nowrap">Rows</th>
          <th scope="col" class="text-right font-mono whitespace-nowrap">Elapsed</th>
        </template>

        <tr v-for="entry in audit.entries.value" :key="entry.id">
          <td class="whitespace-nowrap" :title="fullStamp(entry.createdAt)">
            {{ submittedAt(entry) }}
          </td>
          <td v-if="props.scope === 'all'" class="whitespace-nowrap">{{ entry.operator }}</td>
          <td class="font-mono whitespace-nowrap" :title="entry.instanceId">
            {{ entry.database }}
          </td>
          <td class="w-2/5">
            <pre
              class="statement m-0 max-h-18 overflow-auto font-mono text-xs [word-break:break-word] whitespace-pre-wrap"
              >{{ entry.statement }}</pre
            >
          </td>
          <td>
            <div
              class="verdict flex items-center gap-1.5 font-semibold whitespace-nowrap"
              :class="entry.decision === 'allow' ? 'text-ok' : 'text-danger'"
            >
              <VIcon :name="entry.decision === 'allow' ? 'circle-check' : 'circle-x'" />
              <strong>{{ entry.decision === 'allow' ? 'Allowed' : 'Refused' }}</strong>
            </div>

            <!-- Every reason, in the order the guard produced them. Showing
                 the first alone leaves the operator fixing one problem at a
                 time with no idea how many remain. FEAT-003 AC-8. -->
            <ul v-if="entry.reasons.length > 0" class="reasons mt-1 mb-0 list-none p-0">
              <li
                v-for="(reason, index) in entry.reasons"
                :key="index"
                class="flex items-baseline gap-1.75 py-0.25"
              >
                <span
                  class="rule flex-none rounded-control border border-danger-line bg-danger-bg px-1.25 font-mono text-xs font-semibold text-danger"
                  >{{ reason.rule }}</span
                >
                <span>{{ describeReason(reason) }}</span>
              </li>
            </ul>
          </td>
          <td class="text-right font-mono whitespace-nowrap">
            <span v-if="ran(entry.rowCount)">{{ entry.rowCount }}</span>
            <span v-else class="text-fg-muted" title="The statement never ran">—</span>
          </td>
          <td class="text-right font-mono whitespace-nowrap">
            <span v-if="ran(entry.elapsedMs)">{{ entry.elapsedMs }} ms</span>
            <span v-else class="text-fg-muted" title="The statement never ran">—</span>
          </td>
        </tr>
      </VTable>
    </div>
    <div v-else-if="waiting" class="min-h-0 flex-1 overflow-auto">
      <VSkeleton
        variant="table"
        table="log"
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading the audit log"
      />
    </div>

    <VPager
      :page="audit.page.value"
      :can-go-back="audit.canGoBack.value"
      :has-more="audit.hasMore.value"
      :busy="audit.busy.value"
      @previous="audit.previous()"
      @next="audit.next()"
    >
      Page {{ audit.page.value }} · {{ audit.entries.value.length }} of
      {{ audit.pageSize.value }} per page
    </VPager>
  </div>
</template>
