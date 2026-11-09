import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type WriteKind = components['schemas']['WriteKind']
export type WriteVerdict = components['schemas']['WriteVerdict']

/**
 * FEAT-009's composer, and the reason it is not three text boxes.
 *
 * The predecessor's Update Database Create page picked a database, a table and its
 * columns and wrote the statement's skeleton into the editor. That is the shape
 * an operator can actually use: a production table has forty columns with names
 * nobody types from memory, and asking somebody to write the statement blind is
 * asking them to get it wrong somewhere an approver has to catch.
 *
 * What is different: The predecessor generated the backup half of the statement too,
 * copying the rows into a `tmp_<table>_<timestamp>_<user>` table by hand.
 * FEAT-009 takes the backup inside the write's own transaction, so there is
 * nothing left for the operator to remember — and a template that offered the
 * manual version would be offering them a way to forget it.
 */
export const writeKinds: { value: WriteKind; label: string }[] = [
  { value: 'update', label: 'UPDATE' },
  { value: 'delete', label: 'DELETE' },
  { value: 'insert', label: 'INSERT' },
]

/** A DELETE removes whole rows and names no column. */
export function needsColumns(kind: WriteKind): boolean {
  return kind !== 'delete'
}

export function useWriteComposer() {
  const kind = ref<WriteKind>('update')
  const statement = ref('')
  const reason = ref('')

  /** What the guard said about the statement as it stands, before submitting. */
  const verdict = ref<WriteVerdict | null>(null)
  /**
   * A refusal that is not a guard rule: the governed server would not compile
   * the statement, or would not accept it.
   *
   * Kept apart from `failure`, which is Avior breaking. `"x"` is an identifier
   * in PostgreSQL and somebody meaning a string writes it every day; reading
   * that as a fault sends them to whoever runs Avior with a question about
   * their own typing.
   */
  const refusal = ref<{ code: string; message: string } | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  const allowed = computed(() => verdict.value?.decision === 'allow')
  const checked = computed(() => verdict.value !== null)

  /**
   * Any edit clears the verdict.
   *
   * A verdict shown beside a statement it was not about is worse than no
   * verdict: it is an answer to a question nobody asked, and the number beside
   * it is how many rows some other statement would have touched.
   */
  function edited(): void {
    verdict.value = null
    refusal.value = null
  }

  async function template(schema: string, table: string, columns: string[]): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const body = needsColumns(kind.value)
        ? { kind: kind.value, schema, table, columns }
        : { kind: kind.value, schema, table }
      const { data, error, response } = await api.POST('/writes/template', { body })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      statement.value = data.statement
      edited()
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function check(database: string): Promise<void> {
    busy.value = true
    failure.value = null
    refusal.value = null
    verdict.value = null
    try {
      const { data, error, response } = await api.POST('/writes/inspect', {
        body: { database, statement: statement.value },
      })
      if (data === undefined) {
        if (response.status === 422) {
          refusal.value = error
          return
        }
        failure.value = classify(response.status, error)
        return
      }
      verdict.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  return {
    kind,
    statement,
    reason,
    verdict,
    refusal,
    allowed,
    checked,
    failure,
    busy,
    template,
    check,
    edited,
  }
}
