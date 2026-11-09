import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type WriteRequest = components['schemas']['WriteRequest']
export type WriteRequestDetail = components['schemas']['WriteRequestDetail']
export type WriteRequestState = components['schemas']['WriteRequestState']
export type WriteRefusal = components['schemas']['WriteRefusal']

/**
 * FEAT-009. A developer asks for a row change; an approver reads what it would
 * touch and lets it run or refuses it.
 *
 * Which list a reader gets is the server's decision, not this file's: a caller
 * holding `write.approve` is answered with everybody's requests and a developer
 * with their own. The page renders whatever came back, so a permission change
 * takes effect without anything here knowing about it.
 */
export type WriteScope = 'mine' | 'queue'

/** The states each list is about. */
export const scopeStates: Record<WriteScope, WriteRequestState[]> = {
  mine: ['pending', 'rejected', 'withdrawn', 'applied', 'failed'],
  queue: ['pending'],
}

/** A request that has been decided cannot be decided again. */
export function isSettled(state: WriteRequestState): boolean {
  return state !== 'pending'
}

/**
 * How an estimate reads to somebody deciding.
 *
 * An approver reading `10` and an approver reading `1.4 million` are looking at
 * different decisions, and the difference has to survive being a number in a
 * table cell. Anything past a thousand rows is called out rather than left for
 * the reader to count digits.
 */
export function isLarge(rows: number): boolean {
  return rows > 1000
}

/**
 * Undoing a write, which is a write of its own.
 *
 * A free function rather than a method on one composable, because two pages
 * offer it — the request somebody is reading, and the backup listing where they
 * can see what is still there to put back — and each keeps its own failure
 * state. The server generates the statement; nothing here composes one.
 */
export async function rollbackWrite(
  id: string,
  reason: string,
): Promise<{
  detail?: WriteRequestDetail
  conflict?: { code: string; message: string }
  status: number
}> {
  const { data, error, response } = await api.POST('/writes/{writeId}/rollback', {
    params: { path: { writeId: id } },
    body: { reason },
  })
  if (data !== undefined) return { detail: data, status: response.status }
  if (response.status === 409 || response.status === 422) {
    return { conflict: error, status: response.status }
  }
  return { status: response.status }
}

export function useWriteRequests(scope: () => WriteScope = () => 'mine') {
  const requests = ref<WriteRequest[]>([])
  const detail = ref<WriteRequestDetail | null>(null)
  const state = ref<WriteRequestState | ''>('')
  /** Narrow the list to the reader's own, which is a question rather than the
   * default: the page answers "who asked for what" first. */
  const mine = ref(false)

  const failure = ref<Failure | null>(null)
  /** The guard's answer to a submission, which is not a failure of the page. */
  const refusal = ref<WriteRefusal | null>(null)
  const conflict = ref<{ code: string; message: string } | null>(null)
  const busy = ref(false)
  /** Which request is mid-decision, so one row can be disabled on its own. */
  const deciding = ref('')
  /** Which request's detail is on its way, kept apart from busy so the list is not redrawn as loading. */
  const opening = ref('')
  /** The request being read, whether or not its detail has arrived. */
  const selected = ref('')
  /**
   * Every detail read since the list was last loaded, so going back to a
   * request shows it at once rather than asking again. Dropped on every load:
   * a reload is somebody asking for what is true now.
   */
  let details = new Map<string, WriteRequestDetail>()

  function remember(id: string, read: WriteRequestDetail): void {
    details = new Map(details).set(id, read)
    if (selected.value === id) detail.value = read
  }

  const waiting = computed(() => requests.value.filter((item) => item.state === 'pending'))

  function reset(): void {
    failure.value = null
    refusal.value = null
    conflict.value = null
  }

  async function load(): Promise<void> {
    busy.value = true
    reset()
    details = new Map()
    try {
      const query: { limit: number; state?: WriteRequestState; mine?: boolean } = { limit: 100 }
      if (mine.value) query.mine = true
      // The queue is what is waiting, and nothing else. A settled request in it
      // would make "nothing needs you" stop being a readable answer.
      const wanted = state.value === '' && scope() === 'queue' ? 'pending' : state.value
      if (wanted !== '') query.state = wanted

      const { data, error, response } = await api.GET('/writes', { params: { query } })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      requests.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * Submit a statement.
   *
   * Returns the request when one was made. A refusal is not a failure: the
   * guard answering is the feature working, and the reasons are what the
   * requester acts on.
   */
  async function submit(
    database: string,
    statement: string,
    reason: string,
  ): Promise<WriteRequest | null> {
    busy.value = true
    reset()
    try {
      const { data, error, response } = await api.POST('/writes', {
        body: { database, statement, reason },
      })
      if (data === undefined) {
        if (response.status === 422) {
          refusal.value = error as WriteRefusal
          return null
        }
        failure.value = classify(response.status, error)
        return null
      }
      await load()
      return data
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return null
    } finally {
      busy.value = false
    }
  }

  async function fetchDetail(id: string): Promise<void> {
    try {
      const { data, error, response } = await api.GET('/writes/{writeId}', {
        params: { path: { writeId: id } },
      })
      if (selected.value !== id) return
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      remember(id, data)
    } catch (thrown) {
      if (selected.value === id) failure.value = unreachable(thrown)
    } finally {
      if (opening.value === id) opening.value = ''
    }
  }

  /**
   * Read one request.
   *
   * A request read before comes back from memory with no request and no
   * loader. One that has not is busy until it answers, unless the same request
   * is already on screen, which stays there rather than blinking out.
   */
  async function open(id: string): Promise<void> {
    if (selected.value === id && (detail.value !== null || opening.value === id)) return
    reset()
    selected.value = id
    const known = details.get(id)
    if (known !== undefined) {
      opening.value = ''
      detail.value = known
      return
    }
    detail.value = null
    opening.value = id
    await fetchDetail(id)
  }

  /** The list again, and the request being read again, without emptying either first. */
  async function refresh(): Promise<void> {
    await load()
    const id = selected.value
    if (id !== '' && failure.value === null) await fetchDetail(id)
  }

  function close(): void {
    opening.value = ''
    selected.value = ''
    detail.value = null
    reset()
  }

  async function decide(id: string, decision: 'approve' | 'reject', note: string): Promise<void> {
    deciding.value = id
    reset()
    try {
      const { data, error, response } = await api.POST('/writes/{writeId}/decision', {
        params: { path: { writeId: id } },
        body: note === '' ? { decision } : { decision, note },
      })
      if (data === undefined) {
        if (response.status === 409) {
          conflict.value = error
        } else {
          failure.value = classify(response.status, error)
        }
        return
      }
      await load()
      remember(id, data)
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      deciding.value = ''
    }
  }

  async function rollback(id: string, reason: string): Promise<void> {
    deciding.value = id
    reset()
    try {
      const answer = await rollbackWrite(id, reason)
      if (answer.detail !== undefined) {
        await load()
        remember(id, answer.detail)
        return
      }
      if (answer.conflict !== undefined) {
        conflict.value = answer.conflict
        return
      }
      failure.value = classify(answer.status, undefined)
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      deciding.value = ''
    }
  }

  async function withdraw(id: string): Promise<void> {
    deciding.value = id
    reset()
    try {
      const { data, error, response } = await api.POST('/writes/{writeId}/withdrawal', {
        params: { path: { writeId: id } },
      })
      if (data === undefined) {
        if (response.status === 409) {
          conflict.value = error
        } else {
          failure.value = classify(response.status, error)
        }
        return
      }
      await load()
      remember(id, data)
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      deciding.value = ''
    }
  }

  return {
    requests,
    waiting,
    detail,
    state,
    mine,
    failure,
    refusal,
    conflict,
    busy,
    deciding,
    opening,
    selected,
    load,
    refresh,
    submit,
    open,
    close,
    decide,
    rollback,
    withdraw,
  }
}
