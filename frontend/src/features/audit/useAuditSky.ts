import { computed, onMounted, ref, shallowRef, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import type { AuditHistogram, Span } from '@/components/Data/VSkyline/sky'
import {
  inclusiveEnd,
  spanFor,
  windowFromQuery,
  windowLabel,
  windowOf,
  type Window,
} from '@/components/Data/VSkyline/window'
import { api, classify, describeFailure, unreachable } from '@/shared/api'
import type { AuditDecision, AuditScope } from './useAuditLog'

/** The parts of the audit log the sky reads and steers. */
export type SkyLog = {
  brushed: Ref<{ from: string; to: string } | null>
  operator: Ref<string>
  statement: Ref<string>
  decision: Ref<AuditDecision | ''>
  busy: Ref<boolean>
  apply: () => Promise<void>
}

function filtersOf(scope: AuditScope, log: SkyLog) {
  if (scope === 'mine') return {}
  const operator = log.operator.value.trim()
  const statement = log.statement.value.trim()
  return {
    ...(operator === '' ? {} : { operator }),
    ...(statement === '' ? {} : { statement }),
    ...(log.decision.value === '' ? {} : { decision: log.decision.value }),
  }
}

/**
 * The sky over the audit log: the histogram of the same set the table lists,
 * and a brushed window that narrows the table, lives in the URL, and comes back
 * with it.
 */
export function useAuditSky(scope: () => AuditScope, log: SkyLog) {
  const route = useRoute()
  const router = useRouter()
  const histogram = shallowRef<AuditHistogram | null>(null)
  const failure = ref('')
  let asked = ''
  let latest = 0

  const window = computed(() => windowFromQuery(route.query['from'], route.query['to']))
  const span = computed(() => {
    const drawn = histogram.value
    return drawn === null || window.value === null
      ? null
      : spanFor(window.value, drawn.buckets, drawn.bucketSeconds)
  })
  const label = computed(() => (window.value === null ? '' : windowLabel(window.value)))

  async function load(): Promise<void> {
    const call = ++latest
    const filters = filtersOf(scope(), log)
    asked = JSON.stringify([scope(), filters])
    try {
      const { data, error, response } =
        scope() === 'all'
          ? await api.GET('/audit/histogram', { params: { query: filters } })
          : await api.GET('/audit/mine/histogram')
      if (call !== latest) return
      failure.value = data === undefined ? describeFailure(classify(response.status, error)) : ''
      histogram.value = data ?? null
    } catch (thrown) {
      if (call === latest) failure.value = describeFailure(unreachable(thrown))
    }
  }

  async function narrow(next: Window | null): Promise<void> {
    const rest = Object.fromEntries(
      Object.entries(route.query).filter(([key]) => key !== 'from' && key !== 'to'),
    )
    const query = next === null ? rest : { ...rest, from: next.from, to: next.to }
    await router.replace({ query })
  }

  function follow(next: Window | null): void {
    log.brushed.value = next === null ? null : { from: next.from, to: inclusiveEnd(next.to) }
    void log.apply()
  }

  async function brush(picked: Span): Promise<void> {
    const drawn = histogram.value
    const next = drawn === null ? null : windowOf(picked, drawn.buckets, drawn.bucketSeconds)
    if (next !== null) await narrow(next)
  }

  const clear = () => narrow(null)

  watch(window, (next, before) => {
    if (JSON.stringify(next) !== JSON.stringify(before)) follow(next)
  })

  watch(log.busy, (busy) => {
    if (!busy && JSON.stringify([scope(), filtersOf(scope(), log)]) !== asked) void load()
  })

  onMounted(() => {
    if (window.value !== null) follow(window.value)
    void load()
  })

  return { histogram, failure, span, label, brush, clear }
}
