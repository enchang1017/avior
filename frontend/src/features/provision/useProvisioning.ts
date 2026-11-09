import { computed, ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type Preconditions = components['schemas']['ProvisionPreconditions']
export type ProvisionedObject = components['schemas']['ProvisionedObject']
export type WriteReadiness = components['schemas']['WriteReadiness']
type WriteGap = WriteReadiness['missing'][number]

/** What each gap in the write path is called on the page. FEAT-076 AC-7. */
const writeGapLabels: Record<WriteGap, string> = {
  writer_role: 'the writer role',
  connect: 'CONNECT',
  capture_table: 'the capture table',
  capture_grants: 'grants on the capture table',
  table_grants: 'grants on the tables there now',
  default_privileges: 'default privileges for tables a release adds',
}

export function describeWriteGaps(readiness: WriteReadiness): string {
  return readiness.missing.map((gap) => writeGapLabels[gap]).join(', ')
}

/**
 * The shape a name has to be, checked here as well as at the server.
 *
 * Not because the server's check is in doubt — it is the one that counts — but
 * because a refusal that arrives after a round trip reads as the server being
 * broken, and this one can say what is wrong while somebody is still typing.
 * Identical to the pattern in the contract, deliberately: two rules that
 * disagreed would make the page refuse what the server accepts.
 */
export const namePattern = /^[a-z][a-z0-9_]{0,62}$/

export function nameProblem(name: string): string | null {
  const trimmed = name.trim()
  if (trimmed === '') return 'Name the database.'
  if (namePattern.test(trimmed)) return null
  return 'Lower-case letters, digits and underscores, starting with a letter.'
}

/**
 * useProvisioning reads what a governed database is missing, and creates it.
 *
 * Reading is separate from creating and happens first, always: this page exists
 * because somebody was about to apply a release and wanted to know whether it
 * could land. Creating is what they do having read.
 */
export function useProvisioning() {
  const database = ref('')
  /** The roles a migration grants to, as the submission would declare them. */
  const roles = ref('')

  const preconditions = ref<Preconditions | null>(null)
  const created = ref<ProvisionedObject[]>([])
  const failure = ref<Failure | null>(null)
  const busy = ref(false)

  /** Everything the database is missing, which is what the buttons act on. */
  const missing = computed(() => {
    const report = preconditions.value
    if (report === null) return []
    const gaps: string[] = []
    if (!report.databaseExists) gaps.push('database')
    if (report.ownerRole !== undefined && report.ownerRoleExists === false) gaps.push('owner role')
    if (report.databaseExists && !report.deployerOwnsSchema) gaps.push('deploy membership')
    for (const role of report.roles) if (!role.exists) gaps.push(role.name)
    return gaps
  })

  /** True when a release could land, which is the whole question. */
  const ready = computed(() => preconditions.value !== null && missing.value.length === 0)

  function declaredRoles(): string[] {
    return roles.value
      .split(/[\s,]+/)
      .map((role) => role.trim())
      .filter((role) => role !== '')
  }

  async function load(): Promise<void> {
    const problem = nameProblem(database.value)
    if (problem !== null) {
      failure.value = { kind: 'error', code: 'provision.invalid_name', message: problem }
      return
    }

    busy.value = true
    failure.value = null
    // Cleared rather than kept: a report about the previous database, left on
    // screen beside a new name, is the page saying something untrue.
    preconditions.value = null
    created.value = []
    try {
      const { data, error, response } = await api.GET('/provision/preconditions', {
        params: { query: { database: database.value.trim(), role: declaredRoles() } },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      preconditions.value = data
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function createDatabase(): Promise<void> {
    await create('/provision/databases', database.value)
  }

  async function createRole(name: string): Promise<void> {
    await create('/provision/roles', name)
  }

  async function create(path: '/provision/databases' | '/provision/roles', name: string) {
    const problem = nameProblem(name)
    if (problem !== null) {
      failure.value = { kind: 'error', code: 'provision.invalid_name', message: problem }
      return
    }
    await act(() => api.POST(path, { body: { name: name.trim() } }))
  }

  /** FEAT-076 AC-2. The database is the one the report is about, not the field. */
  async function grantConnect(role: string): Promise<void> {
    const report = preconditions.value
    if (report === null) return
    await act(() =>
      api.POST('/provision/databases/{name}/connect', {
        params: { path: { name: report.database } },
        body: { role },
      }),
    )
  }

  /** FEAT-076 AC-8. */
  async function prepareWrites(): Promise<void> {
    const report = preconditions.value
    if (report === null) return
    await act(() =>
      api.POST('/provision/databases/{name}/writes', {
        params: { path: { name: report.database } },
      }),
    )
  }

  type Answer = {
    data?: { objects: ProvisionedObject[] }
    error?: unknown
    response: Response
  }

  async function act(request: () => Promise<Answer>): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await request()
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return
      }
      created.value = data.objects
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return
    } finally {
      busy.value = false
    }
    // Read back rather than assume. What was created is what the server says
    // it created, and the page's next answer has to come from the server too:
    // a button that updated the screen from its own request would show a
    // database as ready on the strength of having asked for one.
    await load()
  }

  return {
    database,
    roles,
    preconditions,
    created,
    failure,
    busy,
    missing,
    ready,
    load,
    createDatabase,
    createRole,
    grantConnect,
    prepareWrites,
  }
}
