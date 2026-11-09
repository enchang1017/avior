<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import {
  useReleaseQueue,
  type Release,
  type ReleaseState,
  type ReleaseScope,
} from './useReleaseQueue'
import {
  VBadge,
  VBanner,
  VButton,
  VEmpty,
  VField,
  VFilterBar,
  VPageHeader,
  VPager,
  VScopeSearch,
  VSkeleton,
  VTable,
  VTabs,
  parseQuery,
} from '@/components'
import { describeFailure } from '@/shared/api'
import { releaseScopes, releaseTabs, visibleReleases } from './releaseFilters'
import { stateTone } from './verdict'
import VTrajectory from '@/components/Data/VTrajectory/VTrajectory.vue'
import { releaseTrajectory } from '@/components/Data/VTrajectory/trajectory'
import { fullStamp, stamp } from '@/shared/timestamp'

const props = defineProps<{ scope: ReleaseScope }>()

// The scope arrives as a getter for the reason the audit log's does: two routes
// share one component and Vue Router reuses the instance across them rather
// than remounting, so a scope captured at setup would leave the page reading
// whichever list was opened first.
const queue = useReleaseQueue(() => props.scope)
const router = useRouter()

/*
 * A release is read on its own page, never over the list. Its drift check,
 * lint and apply are a workflow of their own, and a decision taken from a
 * summary beside the list would be taken without them (ADR 0012).
 */
function review(release: Release) {
  return { name: 'release-detail', params: { id: release.id } }
}

function go(release: Release): void {
  void router.push(review(release))
}

const bannerText = computed(() => {
  const failure = queue.failure.value
  return failure === null ? null : describeFailure(failure)
})

/*
 * The databases to offer, taken from the entries themselves.
 *
 * Avior keeps no list of governed databases — FEAT-012 is where one would come
 * from — so the honest source is what the ledger actually mentions. A database
 * nothing was ever released to has nothing to filter to.
 */
/*
 * Issue-style (FEAT-065 AC-11): status tabs counted from the page loaded, and
 * a search whose state:, database: and requester: words become chips. Both
 * narrow what the page holds; the pager still pages through the server.
 */
const tab = ref<ReleaseState | ''>('')
const search = ref('')
const tabs = computed(() => releaseTabs(queue.releases.value, props.scope))
const shown = computed(() =>
  visibleReleases(queue.releases.value, tab.value, parseQuery(search.value, releaseScopes)),
)

function submittedAt(release: Release): string {
  return stamp(release.submittedAt)
}

const heading = computed(() =>
  props.scope === 'pending' ? 'Waiting for a decision' : 'What reached each database',
)

// The two lists are one component over two routes, so a scope change is a prop
// change rather than a remount and nothing would reload without this.
watch(
  () => props.scope,
  () => {
    queue.state.value = ''
    void queue.applyFilters()
  },
)

onMounted(() => void queue.load())
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-ground">
    <VPageHeader
      icon="release"
      :title="heading"
      :count="queue.releases.value.length"
      :busy="queue.busy.value"
    />

    <VFilterBar :busy="queue.busy.value" label="Refresh" @submit="queue.applyFilters()">
      <VField label="Search" for="release-search" wide>
        <VScopeSearch
          id="release-search"
          v-model="search"
          :keys="releaseScopes"
          placeholder="state:, database:, requester: or any word"
        />
      </VField>
    </VFilterBar>

    <VTabs v-model="tab" :tabs="tabs" label="Release state" class="flex-none px-4" />

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>

    <div class="rows min-h-0 flex-1 overflow-auto">
      <VTable v-if="shown.length > 0" variant="list">
        <template #caption>{{ heading }}, newest first</template>
        <template #head>
          <th scope="col">State</th>
          <th scope="col">Database</th>
          <th scope="col">Commit</th>
          <th scope="col">Author</th>
          <th scope="col" class="numeric text-right font-mono whitespace-nowrap">Files</th>
          <th scope="col">Submitted</th>
          <th scope="col"><span class="sr-only">Review</span></th>
        </template>
        <tr
          v-for="release in shown"
          :key="release.id"
          class="cursor-pointer"
          :data-release="release.id"
          @click="go(release)"
        >
          <td>
            <VBadge :tone="stateTone[release.state]">{{ release.state }}</VBadge>
            <VTrajectory :trajectory="releaseTrajectory(release.state)" class="mt-1" />
          </td>
          <td class="database font-mono whitespace-nowrap">{{ release.database }}</td>
          <td class="commit font-mono whitespace-nowrap">{{ release.commitSha.slice(0, 10) }}</td>
          <td class="author whitespace-nowrap">{{ release.author }}</td>
          <td class="numeric text-right font-mono whitespace-nowrap">
            {{ release.migrationCount ?? 0 }}
          </td>
          <td class="when whitespace-nowrap" :title="fullStamp(release.submittedAt)">
            {{ submittedAt(release) }}
          </td>
          <td>
            <VButton :to="review(release)" @click.stop>Review</VButton>
          </td>
        </tr>
      </VTable>

      <!-- Before "nothing is waiting", which is a different answer and the one
           an operator acts on. A list that has not arrived must not read as a
           list that is empty. -->
      <VSkeleton v-else-if="queue.busy.value" :lines="5" label="Reading the queue" />

      <VEmpty v-else-if="queue.releases.value.length > 0">Nothing here matches.</VEmpty>
      <VEmpty v-else>
        {{ props.scope === 'pending' ? 'Nothing is waiting.' : 'Nothing has been settled yet.' }}
      </VEmpty>
    </div>

    <VPager
      :page="queue.page.value"
      :can-go-back="queue.canGoBack.value"
      :has-more="queue.hasMore.value"
      @previous="queue.previous()"
      @next="queue.next()"
    />
  </div>
</template>
