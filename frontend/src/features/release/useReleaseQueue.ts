import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type Release = components['schemas']['Release']
export type ReleaseDetail = components['schemas']['ReleaseDetail']
export type ReleaseState = components['schemas']['ReleaseState']
export type DriftCheck = components['schemas']['DriftCheck']

export const requestedPageSize = 50

/**
 * Which list is being read.
 *
 * `pending` is what is waiting for somebody: awaiting a decision, or decided
 * and awaiting an apply — an approval is not the end of the job. `history` is
 * what already happened, whatever the outcome.
 *
 * They are two questions rather than one list with a filter, because an empty
 * pending list means "nothing needs you" and stops meaning that the moment it
 * is mixed into a history.
 */
export type ReleaseScope = 'pending' | 'history'

/** The states each list covers. Together they are every state, exactly once. */
export const scopeStates: Record<ReleaseScope, ReleaseState[]> = {
  pending: ['pending', 'approved'],
  history: ['applied', 'failed', 'rejected'],
}

/**
 * A drift result an approver has to act on before anything else on the page.
 *
 * `unavailable` sits here with `drifted` deliberately. It means the digest could
 * not be computed, which is not agreement — a check that could not run tells
 * nobody anything, and rendering it as an absence is how somebody approves
 * against a database nobody looked at.
 */
export function driftBlocks(check: DriftCheck | null | undefined): boolean {
  if (check === null || check === undefined) return false
  return check.result !== 'match'
}

/** A release that has been decided cannot be decided again. */
export function isSettled(state: ReleaseState): boolean {
  return state !== 'pending'
}

/**
 * useReleaseQueue reads the release ledger and acts on one entry.
 *
 * Paging steps by the page size the response reports rather than the one that
 * was asked for: a limit above the server's maximum comes back clamped, and
 * stepping by the requested size would skip every row between the two.
 */
export function useReleaseQueue(scope: () => ReleaseScope = () => 'pending') {
  const releases = ref<Release[]>([])
  const pageSize = ref(requestedPageSize)
  const hasMore = ref(false)
  const offset = ref(0)

  const state = ref<ReleaseState | ''>('')
  const database = ref('')

  const detail = ref<ReleaseDetail | null>(null)
  const failure = ref<Failure | null>(null)
  const forbidden = ref(false)
  const conflict = ref<{ code: string; message: string } | null>(null)
  const busy = ref(false)

  const canGoBack = computed(() => offset.value > 0)
  const page = computed(() => Math.floor(offset.value / pageSize.value) + 1)

  /** The drift check on the entry being read, when one has run. */
  const drift = computed<DriftCheck | null>(() => detail.value?.driftCheck ?? null)

  /** Whether the entry being read is blocked from applying by its drift check. */
  const driftIsBlocking = computed(() => driftBlocks(drift.value))

  /** The states this list covers, narrowed further when a filter is set. */
  const states = computed(() => {
    const covered = scopeStates[scope()]
    return state.value === '' ? covered : covered.filter((s) => s === state.value)
  })

  function query(forState: ReleaseState) {
    const named = database.value.trim()
    return {
      limit: requestedPageSize,
      offset: offset.value,
      state: forState,
      ...(named === '' ? {} : { database: named }),
    }
  }

  function clearProblems() {
    failure.value = null
    forbidden.value = false
    conflict.value = null
  }

  /**
   * Reads every state this list covers and merges them, newest first.
   *
   * The endpoint filters on one state, so a list spanning several is several
   * requests. That is deliberate rather than a widened filter: the states each
   * list covers is a decision this feature makes, and pushing it into the API
   * would let any caller ask for a combination nobody designed.
   */
  async function load(): Promise<void> {
    busy.value = true
    clearProblems()
    try {
      const pages = await Promise.all(
        states.value.map((forState) =>
          api.GET('/releases', { params: { query: query(forState) } }),
        ),
      )

      const refused = pages.find((page) => page.data === undefined)
      if (refused !== undefined) {
        // A stale page under a filter that was refused reads as that filter's
        // result, so it goes rather than staying on screen.
        releases.value = []
        hasMore.value = false
        forbidden.value = refused.response.status === 403
        failure.value = classify(refused.response.status, refused.error)
        return
      }

      const merged = pages.flatMap((page) => page.data?.releases ?? [])
      merged.sort((a, b) => b.submittedAt.localeCompare(a.submittedAt))
      releases.value = merged
      pageSize.value = pages[0]?.data?.pageSize ?? requestedPageSize
      hasMore.value = pages.some((page) => page.data?.hasMore === true)
    } catch (thrown) {
      releases.value = []
      hasMore.value = false
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function open(id: string): Promise<void> {
    busy.value = true
    clearProblems()
    detail.value = null
    try {
      const { data, error, response } = await api.GET('/releases/{releaseId}', {
        params: { path: { releaseId: id } },
      })
      if (data === undefined) {
        forbidden.value = response.status === 403
        failure.value = classify(response.status, error)
        return
      }
      detail.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * A 409 is not a fault. Each code means something specific an approver can
   * act on — the target moved, somebody already decided, nobody approved it —
   * and collapsing them into one error message would leave them guessing which.
   */
  function readConflict(status: number, body: unknown): boolean {
    if (status !== 409) return false
    const problem = body as { code?: string; message?: string } | undefined
    conflict.value = {
      code: problem?.code ?? 'release.conflict',
      message: problem?.message ?? 'This release is not in a state that allows that.',
    }
    return true
  }

  async function decide(id: string, decision: 'approve' | 'reject', note: string): Promise<void> {
    busy.value = true
    clearProblems()
    try {
      const { data, error, response } = await api.POST('/releases/{releaseId}/decision', {
        params: { path: { releaseId: id } },
        body: { decision, ...(note.trim() === '' ? {} : { note: note.trim() }) },
      })
      if (data === undefined) {
        forbidden.value = response.status === 403
        if (!readConflict(response.status, error)) {
          failure.value = classify(response.status, error)
        }
        return
      }
      detail.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * A migration that failed against the target answers 502 with the record
   * rather than an error: Avior did its job, the target refused the change, and
   * the detail says how far it got. It is shown, not thrown away.
   */
  async function apply(id: string): Promise<void> {
    busy.value = true
    clearProblems()
    try {
      const { data, error, response } = await api.POST('/releases/{releaseId}/apply', {
        params: { path: { releaseId: id } },
      })
      if (data === undefined) {
        if (response.status === 502 && error !== undefined) {
          detail.value = error as unknown as ReleaseDetail
          return
        }
        forbidden.value = response.status === 403
        if (!readConflict(response.status, error)) {
          failure.value = classify(response.status, error)
        }
        return
      }
      detail.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /** Puts the release being read away. */
  async function applyFilters(): Promise<void> {
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

  return {
    releases,
    states,
    detail,
    drift,
    driftIsBlocking,
    pageSize,
    hasMore,
    page,
    canGoBack,
    state,
    database,
    failure,
    forbidden,
    conflict,
    busy,
    load,
    open,
    decide,
    apply,
    applyFilters,
    next,
    previous,
  }
}
