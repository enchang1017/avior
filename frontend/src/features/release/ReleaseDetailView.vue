<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import {
  useReleaseQueue,
  isSettled,
  scopeStates,
  type ReleaseDetail,
  type ReleaseScope,
} from './useReleaseQueue'
import {
  VBadge,
  VBanner,
  VButton,
  VCard,
  VIcon,
  VInput,
  VSkeleton,
  VTable,
  VToolbar,
  VTrajectory,
  toneAccent,
  toneLine,
  toneText,
  toneWash,
} from '@/components'
import SqlScript from '@/shared/SqlScript.vue'
import { describeFailure } from '@/shared/api'
import { hasComments, withoutComments } from '@/shared/sqlComments'
import {
  countBySeverity,
  driftVerdict,
  driftTone as toneOfDrift,
  severityTone,
  stateTone,
  verdictFor,
} from './verdict'
import { anySizes, describeBytes, describeRows } from './sizes'
import { releaseStepDetails } from './steps'
import { releaseTrajectory } from '@/components/Data/VTrajectory/trajectory'
import { useSessionStore } from '@/stores/session'
import { fullStamp, stamp } from '@/shared/timestamp'

const props = defineProps<{ id: string }>()

const session = useSessionStore()
const queue = useReleaseQueue()
const note = ref('')
const confirmingReject = ref(false)

/*
 * Comments off by default.
 *
 * A migration file opens with a paragraph explaining itself, and an approver
 * reading ten of them is reading prose to find the statements. Hiding it is a
 * view of the file, never a change to it: the SQL that runs is whatever was
 * submitted, and one click shows that.
 */
const showComments = ref(false)

function bodyOf(sql: string): string {
  return showComments.value ? sql : withoutComments(sql)
}

/**
 * Approving and applying are offered on the permission the endpoints enforce,
 * not on a role. Gating on anything else would offer a button that answers 403.
 */
const canApprove = computed(() => session.caller?.permissions.includes('release.approve') === true)

const detail = computed(() => queue.detail.value)
const release = computed(() => detail.value?.release ?? null)

/*
 * Where the release stands, step by step, with who did each and when. The
 * queue's side sheet used to carry this; the page is where a release is read
 * now, so it carries it instead.
 */
const trajectory = computed(() =>
  release.value === null ? null : releaseTrajectory(release.value.state),
)
const steps = computed(() => (detail.value === null ? [] : releaseStepDetails(detail.value)))

const canDecide = computed(
  () => canApprove.value && release.value !== null && !isSettled(release.value.state),
)
// Approved, or approved and then failed. A failed entry keeps its approval and
// is applied again once whatever stopped it has been dealt with -- usually the
// environment rather than the SQL, and the files that did land are skipped by
// the target's own ledger. FEAT-017 AC-22.
const canApply = computed(
  () =>
    canApprove.value && (release.value?.state === 'approved' || release.value?.state === 'failed'),
)

// The control is offered only when it would do something: a file whose only
// comments are inside a function body has nothing to hide.
const anyComments = computed(
  () => detail.value?.migrations.some((migration) => hasComments(migration.sql)) === true,
)

const bannerText = computed(() => {
  const failure = queue.failure.value
  return failure === null ? null : describeFailure(failure)
})

/*
 * Back goes to the list this release is actually on, which is not always the one
 * the reader arrived from: approving moves an entry out of Pending and applying
 * moves it into History, so a link back to the arrival list would land on a page
 * that no longer holds it. Until the entry loads there is nothing to go on, and
 * Pending is the one somebody is more likely to be working through.
 */
const backTo = computed(() => {
  const state = release.value?.state
  const scope: ReleaseScope =
    state !== undefined && scopeStates.history.includes(state) ? 'history' : 'pending'
  return { name: `release-${scope}` }
})

/**
 * The three drift results are not two. `unavailable` means the digest could not
 * be computed, which is not agreement — rendering it as a warning or an absence
 * is how somebody approves against a database nobody checked.
 */
const driftText = computed(() => {
  const check = queue.drift.value
  if (check === null) return 'No drift check has run yet. One runs before any apply.'
  switch (check.result) {
    case 'match':
      return 'The target holds the schema this migration was generated against.'
    case 'drifted':
      return 'The target has moved. Somebody changed it outside this pipeline, and the migration was generated against a state that no longer exists.'
    default:
      return 'The target’s schema could not be read, so nothing was checked. This is not a pass: applying would be applying against a database nobody looked at.'
  }
})

const driftIcon = computed(() => (queue.driftIsBlocking.value ? 'warning' : 'circle-check'))

const driftTone = computed(() => toneOfDrift(queue.drift.value))
const driftVerdictLabel = computed(() => driftVerdict(queue.drift.value))
const lintCounts = computed(() => countBySeverity(detail.value?.lintFindings ?? []))

/*
 * How big is the table?
 *
 * `CREATE INDEX` without `CONCURRENTLY` reaches production under approval, and
 * how long it holds the lock depends on this and on nothing else on this page.
 * Shown beside the SQL rather than behind a click, because an approver who has
 * to go and look is an approver who will stop looking. FEAT-017 AC-21.
 */
const relationSizes = computed(() => detail.value?.relationSizes ?? [])
const showSizes = computed(() => anySizes(detail.value?.relationSizes))
const lintTone = computed(() => lintCounts.value[0]?.tone ?? 'ok')

const verdict = computed(() =>
  verdictFor(
    release.value,
    queue.drift.value,
    detail.value?.lintFindings ?? [],
    release.value !== null && isSettled(release.value.state),
    detail.value?.readiness ?? null,
  ),
)

/*
 * The one loud thing on the page. Full width, its own colour, and a headline
 * in two words so it is read before it is looked at.
 */
const verdictClasses = computed(() => {
  const tone = verdict.value?.tone ?? 'neutral'
  return [toneLine[tone], toneAccent[tone], toneWash[tone], toneText[tone]]
})

const at = (value: string): string => stamp(value)

function describeApproval(approval: ReleaseDetail['approvals'][number]): string {
  const decided = `${approval.decision}d this ${at(approval.decidedAt)}`
  return approval.note === undefined || approval.note === ''
    ? decided
    : `${decided}: ${approval.note}`
}

/**
 * A failed application says how far it got. Earlier files committed, and
 * reporting only "failed" would misrepresent what the target now holds.
 */
function describeApplication(application: ReleaseDetail['applications'][number]): string {
  const files = application.filesApplied ?? 0
  const parts = [`${application.result} at ${at(application.startedAt)}`, `${files} file(s)`]
  if (application.durationMs !== null && application.durationMs !== undefined) {
    parts.push(`in ${application.durationMs}ms`)
  }
  const line = parts.join(', ')
  return application.error === undefined || application.error === ''
    ? line
    : `${line}: ${application.error}`
}

async function approve() {
  await queue.decide(props.id, 'approve', note.value)
}

async function reject() {
  confirmingReject.value = false
  await queue.decide(props.id, 'reject', note.value)
}

onMounted(() => void queue.open(props.id))
</script>

<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-ground">
    <!-- "Working" only once there is something on the page to work on. The same
         flag covers the first read and every action after it, and during the
         first read the skeleton below is already saying what is happening --
         two indicators saying different words about one request is worse than
         either alone. -->
    <VToolbar :busy="queue.busy.value && release !== null" busy-label="Working">
      <template #lead>
        <VButton variant="bare" icon="arrow-left" :to="backTo">Back to the list</VButton>
      </template>
    </VToolbar>

    <VBanner v-if="bannerText">{{ bannerText }}</VBanner>
    <VBanner v-if="queue.forbidden.value">
      Approving and applying a release need the release.approve permission. This page is readable
      without it.
    </VBanner>
    <VBanner v-if="queue.conflict.value">{{ queue.conflict.value.message }}</VBanner>

    <!-- The body is emptied at the start of every open(), so without this the
         page is blank while the read is in flight and the only sign of it is a
         spinner in the bar above. A release detail is the slowest page here --
         it carries every file's SQL -- so it is the one where that blank is
         long enough to read as broken. -->
    <VSkeleton v-if="release === null && queue.busy.value" :lines="9" label="Reading the release" />

    <div
      v-else-if="release !== null && detail !== null"
      class="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3"
    >
      <!-- Above the release itself: what an approver came to find out, before
           they read anything else. Gone once the release is settled, when the
           question is no longer being asked. -->
      <section
        v-if="verdict"
        class="verdict flex items-start gap-3 rounded-card border border-l-4 px-4 py-3.5"
        :class="verdictClasses"
      >
        <VIcon :name="verdict.icon" size="lg" />
        <div>
          <strong class="block text-lg">{{ verdict.headline }}</strong>
          <span class="text-sm text-fg">{{ verdict.detail }}</span>
        </div>
      </section>

      <VCard as="section" pad>
        <div class="flex flex-col gap-0.25">
          <p class="eyebrow">Release</p>
          <h1 class="m-0 flex items-center gap-2 font-mono text-xl font-bold">
            <VBadge :tone="stateTone[release.state]">{{ release.state }}</VBadge>
            {{ release.database }}
          </h1>
        </div>
        <dl class="mt-3 mb-0 flex flex-wrap gap-5">
          <div class="flex min-w-0 flex-col gap-0.5">
            <dt class="eyebrow">Commit</dt>
            <dd class="m-0 font-mono text-sm wrap-anywhere">{{ release.commitSha }}</dd>
          </div>
          <div class="flex min-w-0 flex-col gap-0.5">
            <dt class="eyebrow">Author</dt>
            <dd class="m-0 text-sm wrap-anywhere">{{ release.author }}</dd>
          </div>
          <div class="flex min-w-0 flex-col gap-0.5">
            <dt class="eyebrow">Submitted</dt>
            <dd class="m-0 text-sm wrap-anywhere" :title="fullStamp(release.submittedAt)">
              {{ at(release.submittedAt) }}
            </dd>
          </div>
          <div v-if="release.mergeRequestUrl" class="flex min-w-0 flex-col gap-0.5">
            <dt class="eyebrow">Merge request</dt>
            <dd class="m-0 text-sm wrap-anywhere">
              <a class="underline" :href="release.mergeRequestUrl">{{ release.mergeRequestUrl }}</a>
            </dd>
          </div>
        </dl>
      </VCard>

      <VCard v-if="trajectory" as="section" pad class="approval-steps">
        <h2 class="mt-0 mb-3 flex items-center gap-1.5 text-sm font-semibold">Approval steps</h2>
        <VTrajectory :trajectory="trajectory" layout="steps" :details="steps" />
      </VCard>

      <!-- The cards stay the page's own surface and carry their status on one
           edge. Filling them would make three coloured blocks competing with
           the verdict. -->
      <VCard as="section" pad :tone="driftTone">
        <div class="flex items-center justify-between gap-4">
          <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">
            <VIcon :name="driftIcon" />
            Drift check
          </h2>
          <VBadge :tone="driftTone">{{ driftVerdictLabel }}</VBadge>
        </div>
        <p class="text-sm">{{ driftText }}</p>
        <p v-if="queue.drift.value" class="text-xs text-fg-soft">
          Checked {{ at(queue.drift.value.checkedAt) }}. It runs again immediately before an apply,
          because the target can move between a decision and a write.
        </p>
      </VCard>

      <VCard v-if="detail.lintFindings.length > 0" as="section" pad :tone="lintTone">
        <div class="flex items-center justify-between gap-4">
          <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">Lint findings</h2>
          <div class="flex gap-1.5">
            <VBadge v-for="group in lintCounts" :key="group.severity" :tone="group.tone">
              {{ group.count }} {{ group.severity }}
            </VBadge>
          </div>
        </div>
        <ul class="m-0 flex list-none flex-col gap-1.5 p-0">
          <li
            v-for="(finding, index) in detail.lintFindings"
            :key="index"
            class="flex flex-wrap items-baseline gap-1.5 text-sm"
          >
            <VBadge :tone="severityTone[finding.severity] ?? 'neutral'">
              {{ finding.severity }}
            </VBadge>
            <span class="font-mono text-xs">{{ finding.rule }}</span>
            <span v-if="finding.object" class="font-mono text-fg-soft">{{ finding.object }}</span>
            <span class="text-sm">{{ finding.message }}</span>
          </li>
        </ul>
      </VCard>

      <VCard v-if="showSizes" as="section" pad>
        <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">
          <VIcon name="database" />
          What the target holds
        </h2>
        <p class="text-xs text-fg-soft">
          As {{ release.database }} holds it now. An index built without
          <span class="font-mono">CONCURRENTLY</span> holds a write lock for as long as it takes to
          read the table, so this is the number that decides whether the SQL below is a release or
          an outage.
        </p>
        <VTable variant="figures" class="mt-2">
          <template #head>
            <th scope="col">Relation</th>
            <th scope="col">Estimated rows</th>
            <th scope="col">On disk</th>
          </template>
          <tr v-for="size in relationSizes" :key="size.relation">
            <td class="font-mono">{{ size.relation }}</td>
            <!-- A figure the target could not give is a word, never a 0, and it
                 is greyed and italic so it cannot be skim-read as a measurement. -->
            <td :class="{ 'text-fg-soft italic': size.rows === null }">{{ describeRows(size) }}</td>
            <td :class="{ 'text-fg-soft italic': size.totalBytes === null }">
              {{ describeBytes(size) }}
            </td>
          </tr>
        </VTable>
        <p v-if="!detail.relationsKnown" class="text-xs text-fg-soft">
          One of these files could not be read statement by statement, so this list is incomplete
          rather than short. A relation it names may be missing from it.
        </p>
      </VCard>

      <VCard as="section" pad>
        <div class="flex items-center justify-between gap-4">
          <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">Migrations</h2>
          <VButton
            v-if="anyComments"
            size="sm"
            :icon="showComments ? 'eye-off' : 'eye'"
            @click="showComments = !showComments"
          >
            {{ showComments ? 'Hide comments' : 'Show comments' }}
          </VButton>
        </div>
        <p class="text-xs text-fg-soft">
          This is the SQL that will run, as it was submitted. It is not a diff computed here.
          <template v-if="anyComments && !showComments">
            Comments are hidden here and are still in the file that runs.
          </template>
        </p>
        <article v-for="migration in detail.migrations" :key="migration.filename" class="mt-3">
          <h3 class="mt-0 mb-1 font-mono text-xs font-semibold text-fg-soft">
            {{ migration.filename }}
          </h3>
          <SqlScript :code="bodyOf(migration.sql)" />
        </article>
      </VCard>

      <VCard v-if="detail.approvals.length > 0" as="section" pad>
        <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">Decisions</h2>
        <ul class="m-0 flex list-none flex-col gap-1.5 p-0">
          <li
            v-for="(approval, index) in detail.approvals"
            :key="index"
            class="flex flex-wrap items-baseline gap-1.5 text-sm"
          >
            <strong>{{ approval.approver }}</strong>
            <span class="text-sm">{{ describeApproval(approval) }}</span>
          </li>
        </ul>
      </VCard>

      <VCard v-if="detail.applications.length > 0" as="section" pad>
        <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">Applications</h2>
        <ul class="m-0 flex list-none flex-col gap-1.5 p-0">
          <li
            v-for="(application, index) in detail.applications"
            :key="index"
            class="flex flex-wrap items-baseline gap-1.5 text-sm"
          >
            <strong>{{ application.appliedBy }}</strong>
            <span class="text-sm">{{ describeApplication(application) }}</span>
          </li>
        </ul>
      </VCard>

      <VCard v-if="canApprove" as="section" pad>
        <h2 class="mt-0 mb-2 flex items-center gap-1.5 text-sm font-semibold">Decide</h2>
        <div class="flex max-w-112 flex-col gap-1">
          <label for="release-note" class="eyebrow">Note</label>
          <VInput id="release-note" v-model="note" type="text" placeholder="optional" />
        </div>
        <div class="mt-3 flex flex-wrap gap-2">
          <VButton variant="primary" :disabled="!canDecide || queue.busy.value" @click="approve()">
            Approve
          </VButton>
          <VButton
            danger
            :disabled="!canDecide || queue.busy.value"
            @click="confirmingReject = true"
          >
            Reject
          </VButton>
          <VButton :disabled="!canApply || queue.busy.value" @click="queue.apply(props.id)">
            Apply to {{ release.database }}
          </VButton>
        </div>
        <p
          v-if="confirmingReject"
          class="mx-0 mt-3 mb-0 flex flex-wrap items-center gap-2 rounded-control border border-warn-line bg-warn-bg px-2.5 py-2 text-warn"
        >
          <span class="text-sm">
            A rejection is final. CI produces a new entry rather than this one being revised,
            because somebody may already have read it.
          </span>
          <VButton danger @click="reject()"> Reject anyway </VButton>
          <VButton variant="bare" @click="confirmingReject = false"> Keep it pending </VButton>
        </p>
      </VCard>
    </div>
  </div>
</template>
