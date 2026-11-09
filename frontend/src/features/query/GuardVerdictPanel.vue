<script setup lang="ts">
import { VIcon } from '@/components'
import { describeReason, type GuardVerdict } from '@/shared/api'

const props = defineProps<{ verdict: GuardVerdict }>()

const numbers = new Intl.NumberFormat('en')
</script>

<template>
  <section
    class="verdict grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-5 gap-y-0 bg-surface px-4 py-3 text-sm"
    :class="props.verdict.decision"
  >
    <div
      class="flex items-center gap-1.5 font-semibold whitespace-nowrap"
      :class="props.verdict.decision === 'allow' ? 'text-ok' : 'text-danger'"
    >
      <VIcon :name="props.verdict.decision === 'allow' ? 'circle-check' : 'circle-x'" />
      <strong class="hud">{{ props.verdict.decision === 'allow' ? 'Allowed' : 'Refused' }}</strong>
    </div>

    <!-- Every rule is listed. Showing only the first leaves the operator fixing
         one problem at a time with no idea how many remain. -->
    <ul v-if="props.verdict.reasons.length > 0" class="m-0 list-none p-0">
      <li
        v-for="(reason, index) in props.verdict.reasons"
        :key="index"
        class="flex items-baseline gap-2 py-0.5"
      >
        <!-- The rule name is what the operator quotes when asking why, so it is
             set apart rather than buried in the sentence. -->
        <span
          class="rule flex-none rounded-control border border-danger-line bg-danger-bg px-1.25 font-mono text-xs font-semibold text-danger"
          >{{ reason.rule }}</span
        >
        <span>{{ describeReason(reason) }}</span>
      </li>
    </ul>

    <dl class="m-0 grid grid-cols-[auto_minmax(0,--spacing(88))] gap-x-3 gap-y-1 text-xs">
      <dt class="text-right hud text-fg-muted">Cost</dt>
      <dd class="m-0 overflow-hidden font-mono text-ellipsis whitespace-nowrap">
        {{ props.verdict.estimatedCost ?? '—' }}
      </dd>
      <dt class="text-right hud text-fg-muted">Rows</dt>
      <dd class="m-0 overflow-hidden font-mono text-ellipsis whitespace-nowrap">
        {{ props.verdict.estimatedRows ?? '—' }}
      </dd>
      <dt class="text-right hud text-fg-muted">Tables</dt>
      <dd class="m-0 overflow-hidden font-mono text-ellipsis whitespace-nowrap">
        {{ props.verdict.referencedTables.join(', ') || '—' }}
      </dd>
      <dt class="text-right hud text-fg-muted">Columns</dt>
      <dd class="columns m-0 overflow-hidden font-mono text-ellipsis whitespace-normal">
        {{ props.verdict.referencedColumns.join(', ') || '—' }}
      </dd>
    </dl>

    <!-- FEAT-075. What pruning left: one month of a table partitioned by month
         and every month of it cost the same to write and very different
         amounts to run, and the plan is where that difference shows. -->
    <div
      v-if="props.verdict.partitionScans?.length"
      class="partition-scans col-span-3 mt-2 border-t border-line pt-2 text-xs"
    >
      <section v-for="scan in props.verdict.partitionScans" :key="`${scan.schema}.${scan.name}`">
        <p class="scan-summary m-0 font-mono">
          <span class="font-semibold">{{ scan.schema }}.{{ scan.name }}</span>
          <span class="text-fg-muted"> (partitioned)</span> · scanned {{ scan.scanned.length }} /
          {{ scan.total }} partitions
        </p>
        <ul class="m-0 list-none py-0.5 pl-3">
          <li
            v-for="partition in scan.scanned"
            :key="`${partition.schema}.${partition.name}`"
            class="flex gap-2 font-mono text-fg-soft"
          >
            <span>{{ partition.name }}</span>
            <span class="text-fg-muted">~{{ numbers.format(partition.estimatedRows) }} rows</span>
          </li>
        </ul>
      </section>
    </div>
  </section>
</template>
