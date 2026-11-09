import type { Page, Route } from '@playwright/test'

import type { components } from '@/gen/api'
import { histogram, smallGraph } from '../redesign/fixtures'
import { betColumns, lineageGet } from './lineage'

type Schemas = components['schemas']

/*
 * A fixed API for the parity screenshots.
 *
 * The seeded stack is shared with the rest of the suite, which writes audit rows,
 * requests and releases as it runs, so a picture of it changes with the order the
 * cases ran in. These answers never change, which is what lets a pixel diff
 * mean "the styles moved" and nothing else.
 */

const at = '2026-09-20T08:30:00Z'
const earlier = '2026-09-18T14:05:00Z'

export const administrator: Schemas['CurrentUser'] = {
  id: 'op-admin',
  username: 'developer',
  roles: ['administrator', 'dba', 'developer', 'release_approver', 'update_approver'],
  permissions: [
    'account.manage',
    'audit.read.all',
    'audit.read.own',
    'instance.maintain',
    'instance.read',
    'permission.manage',
    'provision.manage',
    'query.execute',
    'query.inspect',
    'release.approve',
    'threshold.manage',
    'write.approve',
  ],
  features: ['write', 'release', 'provision'],
  environment: 'staging',
  selfApproval: false,
  instance: { id: 'local', name: 'Local PostgreSQL' },
}

const databases = ['aviorfixture', 'aviordeploy', 'ledger']

const tables: Schemas['TableRef'][] = [
  { schema: 'public', name: 'member', kind: 'table' },
  { schema: 'public', name: 'bet', kind: 'table' },
  { schema: 'public', name: 'member_summary', kind: 'view' },
]

const columns: Schemas['ColumnRef'][] = [
  { name: 'id', type: 'bigint', nullable: false, denied: false },
  { name: 'username', type: 'text', nullable: false, denied: false },
  { name: 'email', type: 'text', nullable: true, denied: true },
  { name: 'created_at', type: 'timestamptz', nullable: false, denied: false },
]

const release: Schemas['Release'] = {
  id: 'rel-0001-aaaa-bbbb',
  database: 'aviorfixture',
  commitSha: '3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39',
  mergeRequestUrl: 'https://git.example.test/avior/fixture/-/merge_requests/42',
  author: 'chen.yu',
  state: 'pending',
  migrationChecksum: 'sha256:9f8e7d6c5b4a',
  migrationCount: 2,
  submittedAt: at,
}

const releases: Schemas['Release'][] = [
  release,
  { ...release, id: 'rel-0002-cccc', state: 'approved', author: 'lin.wei', migrationCount: 1 },
  { ...release, id: 'rel-0003-dddd', state: 'applied', submittedAt: earlier },
  { ...release, id: 'rel-0004-eeee', state: 'failed', submittedAt: earlier },
  { ...release, id: 'rel-0005-ffff', state: 'rejected', submittedAt: earlier },
]

const releaseDetail: Schemas['ReleaseDetail'] = {
  release,
  migrations: [
    {
      filename: '20260920_0001_add_member_tier.sql',
      sql: 'ALTER TABLE public.member ADD COLUMN tier text;\nCREATE INDEX CONCURRENTLY member_tier ON public.member (tier);',
    },
    { filename: '20260920_0002_backfill.sql', sql: "UPDATE public.member SET tier = 'basic';" },
  ],
  relationSizes: [
    { relation: 'public.member', inTarget: true, rows: 1_250_000, totalBytes: 734_003_200 },
    { relation: 'public.member_tier', inTarget: false, rows: null, totalBytes: null },
  ],
  relationsKnown: true,
  readiness: { result: 'match', databaseExists: true },
  lintFindings: [
    {
      rule: 'backfill-in-migration',
      severity: 'warning',
      object: 'public.member',
      message: 'An UPDATE over a large table holds its locks for the whole backfill.',
    },
    {
      rule: 'index-concurrently',
      severity: 'advice',
      message: 'Built concurrently, so it cannot run inside a transaction.',
    },
  ],
  verifyResult: { passed: true, detail: 'Applied cleanly to a restore.' },
  driftCheck: { result: 'match', checkedAt: at, expectedDigest: 'abc123', actualDigest: 'abc123' },
  approvals: [{ approver: 'lin.wei', decision: 'approve', note: 'Looks right.', decidedAt: at }],
  applications: [],
}

const auditPage: Schemas['AuditPage'] = {
  entries: [
    {
      id: 'aud-1',
      operator: 'developer',
      instanceId: 'local',
      database: 'aviorfixture',
      statement: 'SELECT id, username FROM public.member LIMIT 10',
      decision: 'allow',
      reasons: [],
      rowCount: 10,
      elapsedMs: 12,
      createdAt: at,
    },
    {
      id: 'aud-2',
      operator: 'developer',
      instanceId: 'local',
      database: 'aviorfixture',
      statement: 'SELECT * FROM public.member',
      decision: 'deny',
      reasons: [
        {
          rule: 'wildcard_select',
          message: 'Name the columns you need.',
          subject: 'public.member',
        },
      ],
      rowCount: null,
      elapsedMs: null,
      createdAt: earlier,
    },
  ],
  pageSize: 50,
  hasMore: false,
}

const writeRequest: Schemas['WriteRequest'] = {
  id: 'wr-1111-2222-3333',
  database: 'aviorfixture',
  statement: "UPDATE public.member SET tier = 'gold' WHERE id = 42",
  kind: 'update',
  targetTable: 'public.member',
  reason: 'Support ticket 8812: restore the tier lost in the migration.',
  requestedBy: 'chen.yu',
  requestedAt: at,
  estimatedRows: 1,
  state: 'pending',
  reversible: false,
}

const writeRequests: Schemas['WriteRequest'][] = [
  writeRequest,
  {
    ...writeRequest,
    id: 'wr-4444-5555-6666',
    state: 'applied',
    decidedBy: 'lin.wei',
    decidedAt: at,
    ranAt: at,
    affectedRows: 1,
    captureRows: 1,
    reversible: true,
    reverseStatement: "UPDATE public.member SET tier = 'basic' WHERE id = 42",
    requestedAt: earlier,
  },
  { ...writeRequest, id: 'wr-7777-8888-9999', state: 'rejected', estimatedRows: 25_000 },
]

const captures: Schemas['WriteCapture'][] = [
  { request: writeRequests[1] ?? writeRequest, ageHours: 40, stale: false },
  { request: { ...writeRequest, id: 'wr-old', state: 'applied' }, ageHours: 400, stale: true },
]

const person = (id: string, username: string, roles: string[], disabled: boolean) => ({
  id,
  username,
  kind: 'person' as const,
  roles,
  disabled,
  createdAt: id === 'op-admin' ? at : earlier,
})

const operators: Schemas['Operator'][] = [
  person('op-admin', 'developer', ['administrator'], false),
  person('op-2', 'chen.yu', ['developer'], false),
  person('op-3', 'lin.wei', ['release_approver', 'dba'], true),
  {
    id: 'op-4',
    username: 'svc-daily-report',
    kind: 'service',
    roles: ['developer'],
    disabled: false,
    createdAt: earlier,
  },
]

const serviceAccounts: Schemas['ServiceAccount'][] = [
  {
    id: 'op-4',
    username: 'svc-daily-report',
    description: 'Posts the daily bet totals to the ops channel. Run by the data team.',
    disabled: false,
    createdBy: 'developer',
    createdAt: earlier,
    roles: ['developer'],
    tokens: [
      {
        id: 'tok-s1',
        name: 'cron',
        createdAt: earlier,
        expiresAt: '2026-12-18T14:05:00Z',
        lastUsedAt: at,
      },
      {
        id: 'tok-s0',
        name: 'first',
        createdAt: earlier,
        expiresAt: '2026-09-19T14:05:00Z',
        revokedAt: at,
      },
    ],
  },
  {
    id: 'op-5',
    username: 'svc-reporter',
    description: 'Answers questions in chat.',
    disabled: true,
    createdBy: 'developer',
    createdAt: earlier,
    roles: [],
    tokens: [],
  },
]

const roles: Schemas['RoleDetail'][] = [
  {
    name: 'administrator',
    label: 'Administrator',
    summary: 'Manages who may do what.',
    administrable: true,
    permissions: [{ name: 'account.manage', summary: 'Grant and revoke roles.' }],
  },
  {
    name: 'developer',
    label: 'Developer',
    summary: 'Reads governed databases and asks for changes.',
    administrable: true,
    permissions: [{ name: 'query.inspect', summary: 'Run read-only statements.' }],
  },
  {
    name: 'dba',
    label: 'DBA',
    summary: 'Maintains the governed server.',
    administrable: true,
    permissions: [{ name: 'instance.maintain', summary: 'Maintain the instance.' }],
  },
  {
    name: 'release_approver',
    label: 'Release approver',
    summary: 'Decides whether a release may run.',
    administrable: true,
    permissions: [{ name: 'release.approve', summary: 'Approve releases.' }],
  },
]

const grants: Schemas['DatabaseGrant'][] = [
  { role: 'developer', database: 'aviorfixture', reason: 'Day to day reads.', grantedAt: at },
  { role: 'dba', database: 'ledger', reason: 'Maintenance.', grantedAt: earlier },
]

const grantHistory: Schemas['DatabaseGrantChange'][] = [
  {
    actor: 'developer',
    role: 'developer',
    database: 'aviorfixture',
    action: 'grant',
    reason: 'Day to day reads.',
    changedAt: at,
  },
]

const columnPolicy: Schemas['ColumnPolicy'] = {
  restrictions: [
    {
      database: 'aviorfixture',
      object: 'public.member',
      column: 'email',
      reason: 'Personal data.',
      createdAt: earlier,
    },
  ],
  unlocks: [
    {
      database: 'aviorfixture',
      object: 'public.member',
      column: 'email',
      role: 'dba',
      reason: 'Incident 77.',
      grantedBy: 'developer',
      grantedAt: at,
      expiresAt: '2026-09-27T08:30:00Z',
      expired: false,
    },
  ],
}

const columnHistory: Schemas['ColumnChange'][] = [
  {
    actor: 'developer',
    database: 'aviorfixture',
    object: 'public.member',
    column: 'email',
    role: 'dba',
    action: 'unlock',
    reason: 'Incident 77.',
    expiresAt: '2026-09-27T08:30:00Z',
    changedAt: at,
  },
]

const tokens: Schemas['ApiToken'][] = [
  { id: 'tok-1', name: 'laptop agent', createdAt: earlier, expiresAt: '2026-12-18T14:05:00Z' },
  {
    id: 'tok-2',
    name: 'old ci',
    createdAt: earlier,
    expiresAt: '2026-10-18T14:05:00Z',
    lastUsedAt: at,
    revokedAt: at,
  },
]

const structure: Schemas['TableStructure'] = {
  schema: 'public',
  name: 'member',
  kind: 'table',
  owner: 'avior_deployer',
  comment: 'One row per registered member.',
  columns: [
    { name: 'id', type: 'bigint', nullable: false, default: "nextval('member_id_seq')" },
    { name: 'username', type: 'text', nullable: false, comment: 'Unique, case-folded.' },
    { name: 'email', type: 'text', nullable: true },
  ],
  indexes: ['CREATE UNIQUE INDEX member_pkey ON public.member USING btree (id)'],
  grants: ['GRANT SELECT ON public.member TO avior_governed'],
}

const objects: Schemas['ObjectRef'][] = [
  { schema: 'public', name: 'member_summary' },
  { schema: 'reporting', name: 'daily_bets' },
]

const definition: Schemas['ObjectDefinition'] = {
  type: 'view',
  schema: 'public',
  name: 'member_summary',
  definition: 'CREATE VIEW public.member_summary AS\n SELECT id, username\n   FROM public.member;',
  grants: ['GRANT SELECT ON public.member_summary TO avior_governed'],
}

const applied: Schemas['AppliedMigrationList'] = {
  database: 'aviorfixture',
  migrations: [
    {
      filename: '20260901_0001_init.sql',
      status: 'recorded',
      appliedAt: earlier,
      relations: ['public.member'],
      relationsKnown: true,
      release: { ...release, state: 'applied' },
    },
    {
      filename: '20260910_0001_hotfix.sql',
      status: 'unaccounted',
      appliedAt: at,
      relationsKnown: false,
    },
    { filename: '20260915_0001_missing.sql', status: 'missing', relationsKnown: true },
  ],
}

const preconditions: Schemas['ProvisionPreconditions'] = {
  database: 'aviorfixture',
  databaseExists: true,
  ownerRole: 'aviorfixture_owner',
  ownerRoleExists: true,
  deployerOwnsSchema: false,
  roles: [
    { name: 'aviorfixture_read', exists: true },
    { name: 'aviorfixture_write', exists: false },
  ],
}

const fixtureGrant = (extra: Partial<Schemas['DatabaseGrantSummary']>) => ({
  database: 'aviorfixture',
  tables: 3,
  select: 0,
  insert: 0,
  update: 0,
  delete: 0,
  canConnect: true,
  owned: 0,
  ...extra,
})

const provisionedRoles: Schemas['ProvisionedRole'][] = [
  {
    name: 'aviorfixture_owner',
    kind: 'owner',
    canLogin: false,
    memberOf: [],
    members: [],
    grants: [fixtureGrant({ owned: 3, select: 3, insert: 3, update: 3, delete: 3 })],
  },
  {
    name: 'aviorfixture_read',
    kind: 'group',
    canLogin: false,
    memberOf: [],
    members: ['svc_reporting'],
    grants: [fixtureGrant({ select: 3 })],
  },
  {
    name: 'aviorfixture_write',
    kind: 'group',
    canLogin: false,
    memberOf: [],
    members: [],
    grants: [fixtureGrant({ canConnect: false, select: 2, insert: 2, update: 2 })],
  },
  {
    name: 'svc_reporting',
    kind: 'login',
    canLogin: true,
    memberOf: ['aviorfixture_read'],
    members: [],
    grants: [fixtureGrant({ select: 3 })],
  },
]

const roleAccess: Schemas['RoleAccess'] = {
  role: 'aviorfixture_write',
  database: 'aviorfixture',
  canConnect: false,
  connectVia: [],
  connectable: [{ database: 'aviorspare', via: ['PUBLIC'] }],
  tables: [
    {
      schema: 'public',
      name: 'bet',
      kind: 'r',
      owner: 'aviorfixture_owner',
      owned: false,
      privileges: [
        { privilege: 'SELECT', via: ['aviorfixture_write'] },
        { privilege: 'INSERT', via: ['aviorfixture_write'] },
      ],
      columns: [],
    },
    {
      schema: 'public',
      name: 'member',
      kind: 'r',
      owner: 'aviorfixture_owner',
      owned: false,
      privileges: [{ privilege: 'SELECT', via: ['aviorfixture_read'] }],
      columns: [{ privilege: 'UPDATE', columns: ['email', 'tier'], via: ['aviorfixture_write'] }],
    },
    {
      schema: 'public',
      name: 'member_summary',
      kind: 'v',
      owner: 'aviorfixture_owner',
      owned: false,
      privileges: [],
      columns: [],
    },
  ],
  functions: [
    {
      schema: 'public',
      name: 'settle(round_id bigint)',
      kind: 'function',
      owner: 'aviorfixture_owner',
      owned: false,
      privileges: [{ privilege: 'EXECUTE', via: ['PUBLIC'] }],
      columns: [],
    },
  ],
  truncated: false,
}

const queryResult: Schemas['QueryResult'] = {
  columns: [
    { name: 'id', type: 'int8' },
    { name: 'username', type: 'text' },
    { name: 'email', type: 'text', masked: true },
  ],
  rows: [
    [1, 'alice', '***'],
    [2, 'bob', '***'],
    [3, null, '***'],
  ],
  rowCount: 3,
  truncated: false,
  elapsedMs: 8,
  auditId: 'aud-9',
  limits: { maxCost: 50, maxRows: 1000000 },
}

/** A release something stands in the way of: drift, a blocking finding, a failed apply. */
const blockedDetail: Schemas['ReleaseDetail'] = {
  ...releaseDetail,
  release: { ...release, id: 'rel-0004-eeee', state: 'failed' },
  readiness: { result: 'drifted', databaseExists: true, detail: 'public.member has changed.' },
  lintFindings: [
    {
      rule: 'drop-column',
      severity: 'blocking',
      object: 'public.member',
      message: 'Dropping a column loses its data.',
    },
    ...releaseDetail.lintFindings,
  ],
  verifyResult: null,
  driftCheck: {
    result: 'drifted',
    checkedAt: at,
    expectedDigest: 'abc123',
    actualDigest: 'def456',
    detail: 'public.member gained a column nobody released.',
  },
  approvals: [],
  applications: [
    {
      appliedBy: 'lin.wei',
      startedAt: at,
      result: 'failed',
      durationMs: 1200,
      filesApplied: 1,
      error: 'deadlock detected',
      invalidIndexes: ['public.member_tier'],
    },
  ],
}

const refusal: Schemas['GuardVerdict'] = {
  decision: 'deny',
  reasons: [
    {
      rule: 'column_denied',
      message: 'This column is restricted.',
      subject: 'public.member.email',
    },
    { rule: 'cost_limit_exceeded', message: 'The plan costs more than the limit allows.' },
  ],
  referencedTables: ['public.member'],
  referencedColumns: ['public.member.id', 'public.member.email'],
  limits: { maxCost: 50, maxRows: 1000000 },
  estimatedCost: 1234.5,
  estimatedRows: 1_250_000,
}

const minted: Schemas['CreatedToken'] = {
  id: 'tok-3',
  name: 'agent',
  createdAt: at,
  expiresAt: '2026-09-27T08:30:00Z',
  token: 'avior_pat_0123456789abcdef0123456789abcdef',
}

const login: Schemas['ProvisionedLogin'] = {
  name: 'svc_billing',
  password: 'correct-horse-battery-staple-42',
  memberOf: ['aviorfixture_read'],
}

type Answer = { status?: number; body: unknown }

export function get(path: string): Answer | undefined {
  const fixed: Record<string, unknown> = {
    '/auth/me': administrator,
    '/schema/databases': databases,
    '/schema/tables': tables,
    '/schema/columns': columns,
    '/admin/schema/tables': tables,
    '/admin/schema/columns': columns.map((column) => column.name),
    '/databases/governed': databases,
    '/databases/grants': grants,
    '/databases/grants/history': grantHistory,
    '/columns/restrictions': columnPolicy,
    '/columns/history': columnHistory,
    '/operators': operators,
    '/service-accounts': serviceAccounts,
    '/roles': roles,
    '/audit': auditPage,
    '/audit/mine': auditPage,
    '/audit/histogram': histogram(),
    '/audit/mine/histogram': histogram(),
    '/schema/graph': smallGraph,
    '/search': { hits: [], limit: 20, indexing: 0 },
    '/releases': { releases, pageSize: 50, hasMore: false },
    '/writes': writeRequests,
    '/writes/captures': captures,
    '/tokens': tokens,
    '/objects': objects,
    '/objects/definition': definition,
    '/objects/table': structure,
    '/provision/preconditions': preconditions,
    '/provision/roles': provisionedRoles,
  }
  if (path in fixed) return { body: fixed[path] }
  if (path === '/releases/rel-0004-eeee') return { body: blockedDetail }
  if (path.startsWith('/releases/')) return { body: releaseDetail }
  if (path.startsWith('/provision/roles/') && path.endsWith('/access')) return { body: roleAccess }
  if (path.startsWith('/databases/') && path.endsWith('/migrations')) return { body: applied }
  if (path.startsWith('/writes/')) {
    const id = path.slice('/writes/'.length)
    const request = writeRequests.find((one) => one.id === id) ?? writeRequest
    return { body: { request, history: [] } }
  }
  return undefined
}

function post(path: string): Answer | undefined {
  if (path === '/query/execute') return { body: queryResult }
  if (path === '/query/template') {
    return { body: { statement: 'SELECT id, username FROM public.member LIMIT 100' } }
  }
  if (path === '/auth/logout') return { status: 204, body: null }
  if (path === '/query/inspect') return { body: refusal }
  if (path === '/tokens') return { status: 201, body: minted }
  if (path.startsWith('/service-accounts/') && path.endsWith('/tokens')) {
    return { status: 201, body: { ...minted, name: 'nightly' } }
  }
  if (path === '/provision/logins') return { status: 201, body: login }
  if (path === '/auth/login') {
    return {
      status: 403,
      body: {
        code: 'auth.invalid_credentials',
        message: 'That username and password do not match.',
      },
    }
  }
  return undefined
}

/** The answers that depend on what was asked: lineage, and one table's columns. */
function queried(path: string, query: URLSearchParams): Answer | undefined {
  if (path === '/schema/columns' && query.get('table') === 'bet') return { body: betColumns }
  return path.startsWith('/lineage/') ? lineageGet(path, query) : undefined
}

async function answer(route: Route, signedIn: boolean, missed: string[]): Promise<void> {
  const url = new URL(route.request().url())
  const path = url.pathname.replace(/^\/api\/v1/u, '')
  if (!signedIn && path === '/auth/me') {
    await route.fulfill({ status: 401, json: { code: 'auth.required', message: 'Sign in.' } })
    return
  }
  const method = route.request().method()
  const found = method === 'GET' ? (queried(path, url.searchParams) ?? get(path)) : post(path)
  if (found === undefined) {
    missed.push(`${method} ${path}`)
    await route.fulfill({ status: 500, json: { code: 'parity.unanswered', message: path } })
    return
  }
  await route.fulfill({ status: found.status ?? 200, json: found.body })
}

/**
 * Answers every API call from the fixtures and returns the calls it could not.
 *
 * The caller asserts that list is empty: a page drawn around an unanswered
 * request is a picture of an error banner, and a baseline of that proves nothing.
 */
export async function serveFixedApi(page: Page, signedIn = true): Promise<string[]> {
  const missed: string[] = []
  await page.route('**/api/v1/**', (route) => answer(route, signedIn, missed))
  return missed
}
