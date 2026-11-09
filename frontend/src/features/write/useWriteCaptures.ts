import { computed, ref } from 'vue'

import { rollbackWrite } from './useWriteRequests'
import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type WriteCapture = components['schemas']['WriteCapture']

/**
 * FEAT-009. The backups that have accumulated, and the one page that removes
 * one.
 *
 * Nothing prunes on a schedule. A capture is the only copy of rows somebody
 * changed, and a job deleting it by age would eventually delete the one
 * somebody needed at the moment they needed it. Seven days is the age at which
 * a capture is marked, not the age at which anything happens to it: the mark is
 * a prompt to look, and looking is the whole of the safety.
 */
export function useWriteCaptures() {
  const captures = ref<WriteCapture[]>([])
  const failure = ref<Failure | null>(null)
  const conflict = ref<{ code: string; message: string } | null>(null)
  const busy = ref(false)
  /** Which capture is mid-removal, so one row can be disabled on its own. */
  const removing = ref('')
  /** Whether only the marked ones are shown. */
  const staleOnly = ref(false)

  const shown = computed(() =>
    staleOnly.value ? captures.value.filter((capture) => capture.stale) : captures.value,
  )

  const staleCount = computed(() => captures.value.filter((capture) => capture.stale).length)

  async function load(): Promise<void> {
    busy.value = true
    failure.value = null
    conflict.value = null
    try {
      const { data, error, response } = await api.GET('/writes/captures', {
        params: { query: { limit: 200 } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      captures.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * Remove one capture's rows.
   *
   * The request keeps its record and reads afterwards as no longer reversible,
   * so the row leaves this page rather than the system.
   */
  async function remove(id: string): Promise<boolean> {
    removing.value = id
    failure.value = null
    conflict.value = null
    try {
      const { data, error, response } = await api.DELETE('/writes/{writeId}/capture', {
        params: { path: { writeId: id } },
      })
      if (data === undefined) {
        if (response.status === 409) {
          conflict.value = error
        } else {
          failure.value = classify(response.status, error)
        }
        return false
      }
      captures.value = captures.value.filter((capture) => capture.request.id !== id)
      return true
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      removing.value = ''
    }
  }

  /**
   * Put the rows back.
   *
   * The row leaves this page on success: its backup is now the rollback's
   * problem, and the request it undid can no longer be undone twice.
   */
  async function undo(id: string, reason: string): Promise<boolean> {
    removing.value = id
    failure.value = null
    conflict.value = null
    try {
      const answer = await rollbackWrite(id, reason)
      if (answer.detail !== undefined) {
        /*
         * Read the list again rather than dropping the row.
         *
         * A rollback is a write of its own and keeps its own backup, so undoing
         * one both marks a row here and adds a new one. Filtering locally
         * showed neither, and the page only told the truth after somebody
         * reloaded it by hand.
         */
        await load()
        return true
      }
      if (answer.conflict !== undefined) {
        conflict.value = answer.conflict
        return false
      }
      failure.value = classify(answer.status, undefined)
      return false
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      removing.value = ''
    }
  }

  return {
    captures,
    shown,
    staleCount,
    staleOnly,
    failure,
    conflict,
    busy,
    removing,
    load,
    remove,
    undo,
  }
}

/** How old, in the words somebody reads rather than in hours. */
export function describeAge(hours: number): string {
  if (hours < 1) return 'just now'
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`
  const days = Math.floor(hours / 24)
  return `${days} day${days === 1 ? '' : 's'} ago`
}
