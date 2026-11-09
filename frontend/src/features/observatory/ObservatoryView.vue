<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import OverviewTab from './OverviewTab.vue'
import { accessFor, openTab, overviewPanels, tabsFor } from './access'
import { useSessionStore } from '@/stores/session'
import { VEmpty, VPageHeader, VTabs } from '@/components'

// Overview is where everybody lands, so only it is in the route's first load
// (FEAT-072 AC-13); the drill-down tabs load when one is opened.
const UsageTab = defineAsyncComponent(() => import('./UsageTab.vue'))
const CapacityTab = defineAsyncComponent(() => import('./CapacityTab.vue'))
const LastUsedTab = defineAsyncComponent(() => import('./LastUsedTab.vue'))
const PerformanceTab = defineAsyncComponent(() => import('./PerformanceTab.vue'))
const RoutinesTab = defineAsyncComponent(() => import('./RoutinesTab.vue'))

const session = useSessionStore()
const route = useRoute()
const router = useRouter()

const access = computed(() => accessFor(session.caller?.permissions ?? []))
const tabs = computed(() => tabsFor(access.value))

// The open tab lives in the URL, so a link can open the page on one of them.
const tab = computed({
  get: () => openTab(route.query['tab'], tabs.value),
  set: (next: string) => {
    void router.replace({ query: { ...route.query, tab: next } })
  },
})

// A drill-down from the overview names its filters in the query; each tab starts from them.
const filter = (key: string): string | undefined => {
  const value = route.query[key]
  return typeof value === 'string' && value !== '' ? value : undefined
}
const sort = computed(() => {
  const asked = filter('sort')
  return asked === 'total' || asked === 'mean' || asked === 'calls' ? asked : undefined
})
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="telescope"
      title="Observatory"
      subtitle="How each database is used and how it is growing"
    />
    <div class="flex-none px-3">
      <VTabs v-if="tabs.length > 0" v-model="tab" :tabs="tabs" label="Observatory views" />
    </div>

    <VEmpty v-if="tabs.length === 0">Your roles open no view of the observatory.</VEmpty>
    <OverviewTab v-else-if="tab === 'overview'" :panels="overviewPanels(access)" />
    <UsageTab
      v-else-if="tab === 'usage'"
      :key="route.fullPath"
      :scope="access.usage"
      :audit-log="access.auditLog"
      :self="session.caller?.id"
      :from="filter('from')"
      :to="filter('to')"
      :operator="filter('operator')"
      :database="filter('database')"
      :reason="filter('reason')"
    />
    <CapacityTab v-else-if="tab === 'capacity'" :database="filter('database')" />
    <LastUsedTab
      v-else-if="tab === 'last-used'"
      :database="filter('database')"
      :flagged="filter('flagged') === '1'"
    />
    <PerformanceTab v-else-if="tab === 'performance'" :sort="sort" :database="filter('database')" />
    <RoutinesTab v-else-if="tab === 'routines'" />
  </div>
</template>
