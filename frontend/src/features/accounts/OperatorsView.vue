<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import RoleGuide from './RoleGuide.vue'
import { fallbackRoles, holdsRole, useOperators, type Operator } from './useOperators'
import {
  VBadge,
  VBanner,
  VButton,
  VEmpty,
  VField,
  VFilterBar,
  VInput,
  VPageHeader,
  VSkeleton,
  VTable,
} from '@/components'
import { describeFailure } from '@/shared/api'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

const page = useOperators()
const session = useSessionStore()
const guideOpen = ref(false)

/**
 * The columns, from the server where it has answered and from the names this
 * build knows where it has not.
 *
 * Without the fallback the table has no role columns until a second request
 * lands, and reflows under whoever is already reading it.
 */
const columns = computed(() =>
  page.administrable.value.length > 0
    ? page.administrable.value.map((role) => ({
        name: role.name,
        label: role.label,
        summary: role.summary,
      }))
    : fallbackRoles.map((name) => ({ name, label: name, summary: '' })),
)

const skeletonHeadings = computed(() => [
  'Operator',
  ...columns.value.map((role) => role.label),
  'Sign-in',
  'Added',
])
const skeletonColumns = computed(() => [
  'w-21/25',
  ...columns.value.map(() => 'w-1/3'),
  'w-1/2',
  'w-1/2',
])

const bannerText = computed(() => {
  const failure = page.failure.value
  return failure === null ? null : describeFailure(failure)
})

const callerId = computed(() => session.caller?.id ?? '')
/**
 * Why a switch is not offered, or empty when it is.
 *
 * The rule is the server's and it enforces it whatever this returns. It is
 * mirrored here so the refusal arrives before the click rather than as a banner
 * after it.
 */
function roleWithheld(operator: Operator): string {
  if (operator.disabled) {
    return 'This account is blocked from signing in. Unblock it to change what it holds.'
  }
  return ''
}

function blockWithheld(operator: Operator): string {
  if (operator.id === callerId.value) {
    return 'You cannot block your own account. Another administrator can.'
  }
  return ''
}

function toggleRole(operator: Operator, role: string): void {
  void page.setRole(operator, role, !holdsRole(operator, role))
}

function toggleDisabled(operator: Operator): void {
  void page.setDisabled(operator, !operator.disabled)
}

function createdAt(operator: Operator): string {
  return stamp(operator.createdAt)
}

onMounted(() => {
  void page.load()
  void page.loadRoles()
})
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="users"
      title="Account management"
      :count="page.shown.value.length"
      :busy="page.busy.value"
    >
      <VButton icon="help" @click="guideOpen = true">What each role opens</VButton>
    </VPageHeader>

    <VFilterBar :busy="page.busy.value" @submit="page.load()">
      <VField label="Operator" for="operator-search" wide>
        <VInput
          id="operator-search"
          v-model="page.search.value"
          type="search"
          placeholder="every operator"
          autocomplete="off"
        />
      </VField>
    </VFilterBar>

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

    <div class="min-h-0 flex-1 overflow-auto">
      <VTable v-if="page.shown.value.length > 0">
        <template #caption>Every operator and the roles they hold</template>
        <template #head>
          <th scope="col">Operator</th>
          <th v-for="role in columns" :key="role.name" scope="col" :title="role.summary">
            {{ role.label }}
          </th>
          <th scope="col">Sign-in</th>
          <th scope="col">Added</th>
        </template>

        <tr
          v-for="operator in page.shown.value"
          :key="operator.id"
          :class="{ off: operator.disabled }"
        >
          <!-- Every column is sized to what it holds and `Added` absorbs the rest,
               so the switches sit beside the name they belong to instead of
               across a gap the width of the window. A td cannot be the flex
               container: making it one drops it out of the table's own row
               layout, and the row loses its shared height. -->
          <td class="who w-1/100 whitespace-nowrap">
            <div class="flex items-center gap-1.5">
              <!-- A blocked account is still listed, because it is still an
                   account and somebody has to be able to let it back in. It
                   reads as inactive so it is not mistaken for one that works. -->
              <span
                class="username font-mono"
                :class="{ 'text-fg-soft line-through': operator.disabled }"
                >{{ operator.username }}</span
              >
              <VBadge v-if="operator.disabled" tone="danger">blocked</VBadge>
              <VBadge v-if="operator.id === callerId" tone="primary">you</VBadge>
              <VBadge
                v-if="operator.kind === 'service'"
                class="service"
                tone="agent"
                title="An agent's account. It never signs in, and is never an administrator."
              >
                service
              </VBadge>
              <!-- Named rather than switchable: the column would be a control
                   that is always refused, and this is the fact the
                   administrator needs from the row. -->
              <VBadge v-if="operator.roles.includes('administrator')" tone="neutral">
                administrator
              </VBadge>
            </div>
          </td>

          <td v-for="role in columns" :key="role.name" class="switch w-1/100 whitespace-nowrap">
            <VButton
              size="sm"
              :variant="holdsRole(operator, role.name) ? 'primary' : 'ghost'"
              :icon="holdsRole(operator, role.name) ? 'check' : 'minus'"
              :disabled="page.changing.value === operator.id || roleWithheld(operator) !== ''"
              :title="roleWithheld(operator) || role.summary"
              @click="toggleRole(operator, role.name)"
            >
              {{ holdsRole(operator, role.name) ? 'Held' : 'Grant' }}
            </VButton>
          </td>

          <td class="switch w-1/100 whitespace-nowrap">
            <VButton
              size="sm"
              :variant="operator.disabled ? 'primary' : 'ghost'"
              :icon="operator.disabled ? 'lock' : 'lock-open'"
              :danger="!operator.disabled"
              :disabled="page.changing.value === operator.id || blockWithheld(operator) !== ''"
              :title="
                blockWithheld(operator) ||
                (operator.disabled
                  ? 'Let this account sign in again. It keeps the roles it holds.'
                  : 'Stop this account signing in and end any session it has. Roles are kept.')
              "
              @click="toggleDisabled(operator)"
            >
              {{ operator.disabled ? 'Unblock' : 'Block' }}
            </VButton>
          </td>

          <td
            class="w-full pl-5 whitespace-nowrap text-fg-soft"
            :title="fullStamp(operator.createdAt)"
          >
            {{ createdAt(operator) }}
          </td>
        </tr>
      </VTable>

      <VSkeleton
        v-else-if="page.busy.value"
        variant="table"
        :headings="skeletonHeadings"
        :columns="skeletonColumns"
        label="Reading operators"
      />
      <VEmpty v-else>
        {{
          page.operators.value.length === 0
            ? 'No operators. An account exists because somebody was provisioned one.'
            : 'No operator matches that name.'
        }}
      </VEmpty>
    </div>

    <RoleGuide :open="guideOpen" :roles="page.roles.value" @close="guideOpen = false" />
  </div>
</template>
