import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type AppliedMigration = components['schemas']['AppliedMigration']
export type MigrationStatus = AppliedMigration['status']

/**
 * How a row should read to somebody scanning the page.
 *
 * `recorded` is the ordinary case and everything else is a disagreement between
 * two records of the same event. They are graded rather than all marked alike
 * because they are not equally alarming: an unaccounted file means somebody
 * reached this database outside the pipeline, and a missing one means the
 * ledger claims something the database does not corroborate.
 */
export const statusTone: Record<MigrationStatus, 'ok' | 'warn' | 'danger'> = {
  recorded: 'ok',
  unaccounted: 'danger',
  mismatched: 'danger',
  missing: 'warn',
}

export const statusMeaning: Record<MigrationStatus, string> = {
  recorded: 'Avior applied this file and the database agrees on what it was.',
  unaccounted:
    'The database ran this and Avior never submitted it. Something reached this database by a path Avior does not govern.',
  mismatched:
    'One filename, two bodies. Avior refuses this at apply time, so the database was written to outside the pipeline.',
  missing:
    'An applied release says this ran and the database has no record of it. The two disagree and neither is known to be right.',
}

/**
 * useAppliedMigrations reads what actually reached one governed database.
 *
 * The database is required by the endpoint and not defaulted here. Reading the
 * wrong database's history looks exactly like reading the right one, so the
 * request is not sent until somebody has chosen.
 */
export function useAppliedMigrations() {
  const database = ref('')
  const table = ref('')

  const migrations = ref<AppliedMigration[]>([])
  /**
   * Every relation this database's migrations name, for the filter to offer.
   *
   * Collected from an unfiltered read rather than from the rows on screen: once
   * a filter is applied the rows shrink, and options that disappeared as you
   * used them would make the filter impossible to change your mind about.
   */
  const tables = ref<string[]>([])
  const failure = ref<Failure | null>(null)
  const forbidden = ref(false)
  const unreadable = ref<string | null>(null)
  const busy = ref(false)
  const loaded = ref(false)

  /** The disagreements, which are what somebody opens this page to find. */
  const problems = computed(() =>
    migrations.value.filter((migration) => migration.status !== 'recorded'),
  )

  function clearProblems() {
    failure.value = null
    forbidden.value = false
    unreadable.value = null
  }

  /** Reads the whole history for the current database, only to collect names. */
  async function loadTables(): Promise<void> {
    if (database.value.trim() === '') {
      tables.value = []
      return
    }
    try {
      const { data } = await api.GET('/databases/{database}/migrations', {
        params: { path: { database: database.value.trim() }, query: {} },
      })
      const named = new Set<string>()
      for (const migration of data?.migrations ?? []) {
        for (const relation of migration.relations ?? []) named.add(relation)
      }
      tables.value = [...named].sort()
    } catch {
      // The filter falls back to free text. A failure here is not worth a
      // banner: the page's own read reports what went wrong.
      tables.value = []
    }
  }

  async function load(): Promise<void> {
    if (database.value.trim() === '') return

    busy.value = true
    clearProblems()
    try {
      const named = table.value.trim()
      const { data, error, response } = await api.GET('/databases/{database}/migrations', {
        params: {
          path: { database: database.value.trim() },
          query: named === '' ? {} : { table: named },
        },
      })
      if (data === undefined) {
        // An empty list would be a wrong answer here, not a partial one: a
        // database nobody could read is not a database nothing was applied to.
        migrations.value = []
        forbidden.value = response.status === 403
        if (response.status === 502) {
          unreadable.value =
            (error as { message?: string } | undefined)?.message ??
            'The database could not be read, so this is not an empty history.'
        } else {
          failure.value = classify(response.status, error)
        }
        return
      }
      migrations.value = data.migrations
      loaded.value = true
    } catch (thrown) {
      migrations.value = []
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  return {
    database,
    table,
    tables,
    loadTables,
    migrations,
    problems,
    failure,
    forbidden,
    unreadable,
    busy,
    loaded,
    load,
  }
}
