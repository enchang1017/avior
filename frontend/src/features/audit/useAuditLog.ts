import { computed, ref, watch } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type AuditEntry = components['schemas']['AuditEntry']
export type AuditDecision = AuditEntry['decision']

/**
 * Which log is being read. `mine` is the operator's own submissions and every
 * operator has it; `all` is every operator's and needs `audit.read.all`.
 */
export type AuditScope = 'mine' | 'all'

export const requestedPageSize = 50

/**
 * useAuditLog reads one page of the audit trail.
 *
 * The scope arrives as a getter for the reason the object browser's type does:
 * the personal log and the administrator's log are two routes over one
 * component, and Vue Router reuses the instance across them rather than
 * remounting, so a scope captured at setup would leave the page reading
 * whichever log was opened first.
 *
 * Paging steps by the page size the response reports, never by the size that
 * was asked for. A limit above the server's maximum comes back clamped, and
 * stepping by the requested size would skip every row between the two.
 */
export function useAuditLog(scope: () => AuditScope) {
  const entries = ref<AuditEntry[]>([])
  const pageSize = ref(requestedPageSize)
  const hasMore = ref(false)
  const offset = ref(0)
  const limit = ref(requestedPageSize)

  const from = ref('')
  const to = ref('')
  const operator = ref('')
  const statement = ref('')
  const decision = ref<AuditDecision | ''>('')
  /** Exact instants from the sky's brush; while set, they bound the log instead of the inputs. */
  const brushed = ref<{ from: string; to: string } | null>(null)

  const failure = ref<Failure | null>(null)
  const forbidden = ref(false)
  const busy = ref(false)

  const canGoBack = computed(() => offset.value > 0)
  const page = computed(() => Math.floor(offset.value / pageSize.value) + 1)

  /**
   * A datetime-local value carries no zone, so it is read in the operator's own
   * and sent as an instant. A half-typed value bounds nothing rather than
   * being sent as an unparseable date the server would refuse.
   */
  function instant(local: string): string | null {
    if (local.trim() === '') return null
    const parsed = new Date(local)
    return Number.isNaN(parsed.getTime()) ? null : parsed.toISOString()
  }

  function range() {
    const start = brushed.value?.from ?? instant(from.value)
    const end = brushed.value?.to ?? instant(to.value)
    return {
      limit: limit.value,
      offset: offset.value,
      ...(start === null ? {} : { from: start }),
      ...(end === null ? {} : { to: end }),
    }
  }

  function everyone() {
    const named = operator.value.trim()
    const text = statement.value.trim()
    return {
      ...range(),
      ...(named === '' ? {} : { operator: named }),
      ...(text === '' ? {} : { statement: text }),
      ...(decision.value === '' ? {} : { decision: decision.value }),
    }
  }

  let latest = 0

  async function load(): Promise<void> {
    const call = ++latest
    busy.value = true
    failure.value = null
    forbidden.value = false
    try {
      const { data, error, response } =
        scope() === 'all'
          ? await api.GET('/audit', { params: { query: everyone() } })
          : await api.GET('/audit/mine', { params: { query: range() } })

      if (call !== latest) return
      if (data === undefined) {
        // A stale page under a filter that was refused reads as that filter's
        // result, so it goes rather than staying on screen.
        entries.value = []
        hasMore.value = false
        forbidden.value = response.status === 403
        failure.value = classify(response.status, error)
        return
      }
      entries.value = data.entries
      pageSize.value = data.pageSize
      hasMore.value = data.hasMore
    } catch (thrown) {
      if (call !== latest) return
      entries.value = []
      hasMore.value = false
      failure.value = unreachable(thrown)
    } finally {
      if (call === latest) busy.value = false
    }
  }

  /** Applies the filters as they stand, from the first page. */
  async function apply(): Promise<void> {
    offset.value = 0
    await load()
  }

  async function next(): Promise<void> {
    if (!hasMore.value) return
    offset.value += pageSize.value
    await load()
  }

  async function previous(): Promise<void> {
    if (offset.value === 0) return
    offset.value = Math.max(0, offset.value - pageSize.value)
    await load()
  }

  // The date range holds across both logs; the administrator's filters do not
  // exist on the personal endpoint and would otherwise be waiting, applied but
  // invisible, on the way back.
  watch(scope, () => {
    operator.value = ''
    statement.value = ''
    decision.value = ''
    void apply()
  })

  return {
    entries,
    pageSize,
    hasMore,
    offset,
    limit,
    from,
    to,
    operator,
    statement,
    decision,
    brushed,
    failure,
    forbidden,
    busy,
    canGoBack,
    page,
    load,
    apply,
    next,
    previous,
  }
}
