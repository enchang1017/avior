import type { components } from '@/gen/api'
import {
  galaxy,
  neighbourhood,
  routes,
  snapshot,
  type LineageWorld,
  type Reply,
} from './lineageServer'

type Schemas = components['schemas']
type Node = Schemas['LineageNode']
type Edge = Schemas['LineageEdge']

/** The server's node ids, reproduced here only because this file plays the server. */
const quoted = (identifier: string) => `"${identifier.replace(/"/gu, '""')}"`
const relationId = (kind: Node['kind'], schema: string, name: string) =>
  `${kind}:${quoted(schema)}.${quoted(name)}`

const relation = (kind: Node['kind'], name: string, schema = 'public'): Node => ({
  id: relationId(kind, schema, name),
  kind,
  schema,
  name,
})

const routine = (
  kind: 'function' | 'procedure',
  name: string,
  args = '',
  extra: Partial<Node> = {},
): Node => ({
  id: `${kind}:"public"."${name}"(${args})`,
  kind,
  schema: 'public',
  name,
  arguments: args,
  ...extra,
})

export const nodes = {
  bet: relation('table', 'bet'),
  member: relation('table', 'member'),
  ledger: relation('table', 'ledger'),
  account: relation('table', 'account'),
  betAudit: relation('table', 'bet_audit'),
  legacyLog: relation('table', 'legacy_log'),
  grantLog: relation('table', 'grant_log', 'promo'),
  campaign: relation('table', 'campaign', 'promo'),
  memberBets: relation('view', 'member_bets'),
  betSimple: relation('view', 'bet_simple'),
  bigOpenBets: relation('view', 'big_open_bets'),
  dailyStake: relation('matview', 'daily_stake'),
  betSeq: relation('sequence', 'bet_id_seq'),
  settleBet: routine('function', 'settle_bet', 'bigint, numeric', {
    badges: ['search_path_assumed'],
  }),
  openBetCount: routine('function', 'open_bet_count', 'bigint'),
  openBetCountByDay: routine('function', 'open_bet_count', 'bigint, date'),
  betDefaults: routine('function', 'bet_defaults'),
  trgBetAudit: routine('function', 'trg_bet_audit'),
  archiveBets: routine('function', 'archive_bets'),
  memberTotalStake: routine('function', 'member_total_stake', 'bigint'),
  logEvent: routine('function', 'log_event', 'text'),
  campaignReport: routine('function', 'campaign_report'),
  grantBonus: routine('function', 'grant_bonus', 'integer, bigint, numeric'),
  closeMember: routine('procedure', 'close_member', 'bigint', { badges: ['may_reach_unknown'] }),
  purgeTable: routine('function', 'purge_table', 'text', { badges: ['dynamic'] }),
  constExec: routine('function', 'adv_const_exec', '', { badges: ['dynamic'] }),
  similarity: routine('function', 'similarity', 'text, text', { extension: true }),
  wordSimilarity: routine('function', 'word_similarity', 'text, text', { extension: true }),
}

const columns = (...names: string[]) => names.map((name) => ({ name, restricted: false }))

function edge(
  from: Node,
  to: Node,
  kind: Edge['kind'],
  access: Edge['access'],
  confidence: Edge['confidence'],
  extra: Partial<Edge> = {},
): Edge {
  return { from: from.id, to: to.id, kind, access, confidence, ...extra }
}

const n = nodes
const ledgerEntries = Array.from({ length: 6 }, (_, index) =>
  relation('table', `ledger_entry_${index}`),
)

/** The study's `lineage_scratch` around `public.bet`, as the mockups drew it. */
export const betWorld: LineageWorld = {
  nodes: [...Object.values(nodes), ...ledgerEntries],
  hubs: { [n.ledger.id]: 31 },
  edges: [
    edge(n.bet, n.member, 'foreign_key', 'reference', 'exact', {
      fromColumns: columns('member_id'),
      toColumns: columns('id'),
    }),
    edge(n.account, n.member, 'foreign_key', 'reference', 'exact'),
    edge(n.grantLog, n.member, 'foreign_key', 'reference', 'exact'),
    edge(n.grantLog, n.campaign, 'foreign_key', 'reference', 'exact'),
    edge(n.ledger, n.member, 'foreign_key', 'reference', 'exact'),
    edge(n.settleBet, n.bet, 'routine_write', 'write', 'parsed', {
      toColumns: columns('status', 'payout'),
    }),
    edge(n.settleBet, n.bet, 'routine_read', 'read', 'parsed', {
      toColumns: columns('id', 'stake'),
    }),
    edge(n.settleBet, n.ledger, 'routine_write', 'write', 'parsed'),
    edge(n.settleBet, n.logEvent, 'call', 'reference', 'parsed'),
    edge(n.openBetCount, n.bet, 'routine_read', 'read', 'parsed'),
    edge(n.openBetCountByDay, n.bet, 'routine_read', 'read', 'parsed'),
    edge(n.bet, n.betDefaults, 'trigger', 'reference', 'exact', { events: ['insert'] }),
    edge(n.betDefaults, n.bet, 'routine_write', 'write', 'parsed', {
      toColumns: columns('status'),
    }),
    edge(n.bet, n.trgBetAudit, 'trigger', 'reference', 'exact', {
      events: ['update'],
      fromColumns: columns('status'),
    }),
    edge(n.trgBetAudit, n.bet, 'routine_read', 'read', 'parsed'),
    edge(n.trgBetAudit, n.betAudit, 'routine_write', 'write', 'parsed'),
    edge(n.archiveBets, n.bet, 'routine_write', 'write', 'parsed', { toColumns: columns('*') }),
    edge(n.archiveBets, n.legacyLog, 'routine_write', 'write', 'parsed'),
    edge(n.memberTotalStake, n.bet, 'routine_read', 'read', 'exact'),
    edge(n.memberBets, n.bet, 'view_read', 'read', 'exact'),
    edge(n.memberBets, n.member, 'view_read', 'read', 'exact'),
    edge(n.bigOpenBets, n.memberBets, 'view_read', 'read', 'exact'),
    edge(n.betSimple, n.bet, 'view_read', 'read', 'exact'),
    edge(n.dailyStake, n.bet, 'view_read', 'read', 'exact'),
    edge(n.bet, n.betSeq, 'default', 'read', 'exact', { fromColumns: columns('id') }),
    edge(n.closeMember, n.bet, 'routine_read', 'read', 'parsed'),
    edge(n.closeMember, n.member, 'routine_write', 'write', 'parsed'),
    edge(n.closeMember, n.archiveBets, 'call', 'reference', 'parsed'),
    edge(n.closeMember, n.constExec, 'call', 'reference', 'parsed'),
    edge(n.purgeTable, n.bet, 'routine_read', 'read', 'parsed', { toColumns: columns('id') }),
    edge(n.constExec, n.legacyLog, 'routine_read', 'read', 'parsed'),
    edge(n.logEvent, n.legacyLog, 'routine_write', 'write', 'parsed'),
    edge(n.campaignReport, n.memberBets, 'routine_read', 'read', 'parsed'),
    edge(n.campaignReport, n.campaign, 'routine_read', 'read', 'parsed'),
    edge(n.grantBonus, n.grantLog, 'routine_write', 'write', 'exact'),
    edge(n.grantBonus, n.campaign, 'routine_write', 'write', 'exact', {
      evidence: { excerpt: 'UPDATE promo.campaign SET granted = granted + 1', line: 12 },
    }),
    edge(n.similarity, n.bet, 'routine_read', 'read', 'parsed'),
    edge(n.wordSimilarity, n.bet, 'routine_read', 'read', 'parsed'),
    ...ledgerEntries.map((entry) => edge(entry, n.ledger, 'foreign_key', 'reference', 'exact')),
  ],
}

const entry = (
  object: Node,
  access: 'read' | 'write',
  confidence: Schemas['LineageUsageConfidence'],
  kind: Edge['kind'],
  excerpt: string,
  line?: number,
): Schemas['LineageColumnEntry'] => ({
  object,
  access,
  confidence,
  kind,
  excerpt,
  ...(line === undefined ? {} : { line }),
})

export const betStatus: Schemas['LineageColumn'] = {
  state: 'ready',
  snapshot,
  table: n.bet,
  column: { name: 'status', restricted: false },
  entries: [
    entry(n.settleBet, 'write', 'parsed', 'routine_write', "UPDATE bet SET status = 'settled'", 14),
    entry(n.betDefaults, 'write', 'parsed', 'routine_write', 'NEW.status := …', 3),
    entry(n.archiveBets, 'write', 'parsed', 'routine_write', 'DELETE FROM bet USING member', 9),
    entry(
      n.purgeTable,
      'write',
      'dynamic',
      'routine_write',
      "EXECUTE format('DELETE FROM %I', t)",
      5,
    ),
    entry(n.memberBets, 'read', 'exact', 'view_read', 'exposes it as member_bets.status'),
    entry(n.betSimple, 'read', 'exact', 'view_read', 'exposes it as bet_simple.status'),
    entry(n.trgBetAudit, 'read', 'parsed', 'routine_read', 'NEW.status, OLD.status', 4),
    entry(n.openBetCount, 'read', 'parsed', 'routine_read', "WHERE status = 'open'", 6),
    entry(
      n.closeMember,
      'read',
      'parsed',
      'routine_read',
      "FOR r IN SELECT … WHERE status = 'open'",
      11,
    ),
  ],
  counts: [
    { column: { name: 'id', restricted: false }, reads: 9, writes: 0, foreignKey: false },
    { column: { name: 'member_id', restricted: false }, reads: 8, writes: 0, foreignKey: true },
    { column: { name: 'stake', restricted: false }, reads: 6, writes: 1, foreignKey: false },
    { column: { name: 'status', restricted: false }, reads: 8, writes: 3, foreignKey: false },
    { column: { name: 'payout', restricted: true }, reads: 1, writes: 2, foreignKey: false },
  ],
  summary: { writers: 3, readers: 3, views: 2, triggers: 1, dynamic: 1 },
  triggers: [{ name: 'bet_status_audit', function: n.trgBetAudit, events: ['update'] }],
  indirect: [
    {
      entry: entry(n.bigOpenBets, 'read', 'exact', 'view_read', 'WHERE status = 1'),
      via: [{ kind: 'view_read', node: n.memberBets }],
    },
    {
      entry: entry(
        n.campaignReport,
        'read',
        'parsed',
        'routine_read',
        'SELECT … FROM member_bets',
        7,
      ),
      via: [{ kind: 'view_read', node: n.memberBets }],
    },
  ],
  indirectTotal: 2,
}

/** The whole table bet, as the column request answers it without a column. */
export const betTable: Schemas['LineageColumn'] = {
  state: 'ready',
  snapshot,
  table: n.bet,
  entries: betStatus.entries ?? [],
  counts: betStatus.counts ?? [],
  summary: { writers: 4, readers: 5, views: 3, triggers: 2, dynamic: 1 },
  triggers: [
    { name: 'bet_defaults', function: n.betDefaults, events: ['insert'] },
    { name: 'bet_status_audit', function: n.trgBetAudit, events: ['update'] },
  ],
  indirect: betStatus.indirect ?? [],
  indirectTotal: 2,
}

/** bet.stake reaches more than the panel lists, so the cut is said. */
export const betStake: Schemas['LineageColumn'] = {
  ...betStatus,
  column: { name: 'stake', restricted: false },
  entries: [entry(n.settleBet, 'write', 'parsed', 'routine_write', 'UPDATE bet SET stake = 0', 12)],
  summary: { writers: 1, readers: 0, views: 0, triggers: 0, dynamic: 0 },
  triggers: [],
  indirect: Array.from({ length: 20 }, (_, index) => ({
    entry: entry(
      routine('function', `stake_report_${String(index).padStart(2, '0')}`),
      'read',
      'parsed',
      'routine_read',
      'SELECT stake FROM member_bets',
      index + 1,
    ),
    via: [{ kind: 'view_read', node: n.memberBets }],
  })),
  indirectTotal: 26,
}

/** What `/schema/tables` lists for the lineage database: its tables, views and the materialised view. */
export const betTables: Schemas['TableRef'][] = betWorld.nodes.flatMap((node) =>
  node.kind === 'table' || node.kind === 'view'
    ? [{ schema: node.schema, name: node.name, kind: node.kind }]
    : [],
)

export const betColumns: Schemas['ColumnRef'][] = [
  { name: 'id', type: 'bigint', nullable: false, denied: false },
  { name: 'member_id', type: 'bigint', nullable: false, denied: false },
  { name: 'stake', type: 'numeric(18,4)', nullable: false, denied: false },
  { name: 'status', type: 'text', nullable: false, denied: false },
  { name: 'payout', type: 'numeric(18,4)', nullable: true, denied: true },
]

const footprints: Record<string, Schemas['LineageRoutine']> = {
  [n.settleBet.id]: {
    state: 'ready',
    snapshot,
    routine: n.settleBet,
    status: 'parsed',
    writes: [
      { table: n.bet, columns: columns('status', 'payout'), confidence: 'parsed' },
      { table: n.ledger, columns: [{ name: 'amount', restricted: true }], confidence: 'parsed' },
    ],
    reads: [{ table: n.bet, columns: columns('id', 'stake'), confidence: 'parsed' }],
    statements: [
      {
        line: 14,
        excerpt: "UPDATE bet SET status = 'settled', payout = p_payout WHERE id = p_bet",
      },
      { line: 19, excerpt: 'INSERT INTO ledger (member_id, amount) VALUES (…)' },
    ],
    calls: [n.logEvent],
    transitiveCalls: [],
    triggers: [{ name: 'bet_status_audit', table: n.bet, function: n.trgBetAudit }],
    dynamic: [],
    unmodelled: [],
    unresolved: [],
    mayReachUnknown: false,
  },
  [n.closeMember.id]: {
    state: 'ready',
    snapshot,
    routine: n.closeMember,
    status: 'parsed',
    writes: [{ table: n.member, columns: columns('closed_at'), confidence: 'parsed' }],
    reads: [{ table: n.bet, columns: columns('member_id', 'status'), confidence: 'parsed' }],
    statements: [{ line: 11, excerpt: "FOR r IN SELECT id FROM bet WHERE status = 'open'" }],
    calls: [n.archiveBets],
    transitiveCalls: [n.logEvent],
    triggers: [],
    dynamic: [],
    unmodelled: [{ line: 20, form: 'MERGE', excerpt: 'MERGE INTO member_stats …' }],
    unresolved: ['tmp_closing'],
    mayReachUnknown: true,
  },
  [n.purgeTable.id]: {
    state: 'ready',
    snapshot,
    routine: n.purgeTable,
    status: 'parsed',
    writes: [],
    reads: [],
    statements: [],
    calls: [],
    transitiveCalls: [],
    triggers: [],
    dynamic: [{ line: 5, form: 'EXECUTE', excerpt: "EXECUTE format('DELETE FROM %I', t)" }],
    unmodelled: [],
    unresolved: [],
    mayReachUnknown: false,
  },
}

export function footprint(id: string): Schemas['LineageRoutine'] | undefined {
  const known = footprints[id]
  if (known !== undefined) return known
  const node = betWorld.nodes.find((one) => one.id === id)
  if (node === undefined) return undefined
  return { state: 'ready', snapshot, routine: node, status: 'parsed', reads: [], writes: [] }
}

/** Every lineage answer the fixed API gives, for the parity scenes and the lineage specs. */
export function lineageGet(
  path: string,
  query: URLSearchParams,
  world = betWorld,
): Reply | undefined {
  const ok = (body: unknown): Reply | undefined =>
    body === undefined ? undefined : { status: 200, body }
  if (path === '/lineage/graph') return neighbourhood(world, query)
  if (path === '/lineage/path') return routes(world, query)
  if (path === '/lineage/overview') return galaxy(world)
  if (path === '/lineage/routine') return ok(footprint(query.get('routine') ?? ''))
  if (path === '/lineage/column') {
    const column = query.get('column') ?? ''
    if (column === '') return ok(betTable)
    if (column === 'stake') return ok(betStake)
    const quiet = { writers: 0, readers: 0, views: 0, triggers: 0, dynamic: 0 }
    return ok({
      ...betStatus,
      column: { name: column, restricted: column === 'payout' },
      ...(column === 'status'
        ? {}
        : { entries: [], summary: quiet, triggers: [], indirect: [], indirectTotal: 0 }),
    })
  }
  return undefined
}

/** How a page addresses an object seen in the fixture: by schema, name and kind. */
export function addressOf(node: Node, signature = false): Record<string, string> {
  const routineKind = node.kind === 'function' || node.kind === 'procedure'
  return {
    schema: node.schema,
    name: node.name,
    kind: routineKind ? 'routine' : node.kind,
    ...(routineKind && signature ? { signature: node.arguments ?? '' } : {}),
  }
}

/**
 * A neighbourhood at the drawing cap: 150 nodes and 300 edges, a focus with 50
 * neighbours of every kind and 99 more one hop further.
 */
export function capWorld(): LineageWorld {
  const kinds: Node['kind'][] = ['table', 'view', 'function', 'procedure', 'matview']
  const focus = relation('table', 'hub')
  const near = Array.from({ length: 50 }, (_, index) => {
    const kind = kinds[index % kinds.length] ?? 'table'
    return kind === 'function' || kind === 'procedure'
      ? routine(kind, `near_${index}`, 'bigint')
      : relation(kind, `near_${index}`)
  })
  const far = Array.from({ length: 99 }, (_, index) => relation('table', `far_${index}`))
  const edges: Edge[] = []
  near.forEach((node, index) => {
    const routineWrites = node.kind === 'function' || node.kind === 'procedure'
    edges.push(
      edge(
        node,
        focus,
        routineWrites ? 'routine_write' : 'foreign_key',
        routineWrites ? 'write' : 'reference',
        routineWrites ? 'parsed' : 'exact',
      ),
      edge(
        node,
        focus,
        routineWrites ? 'routine_read' : 'view_read',
        'read',
        index % 2 === 0 ? 'exact' : 'parsed',
      ),
    )
  })
  far.forEach((node, index) => {
    const a = near[index % near.length] as Node
    const b = near[(index * 7 + 3) % near.length] as Node
    edges.push(
      edge(node, a, 'foreign_key', 'reference', 'exact'),
      edge(b, node, 'routine_read', 'read', 'parsed'),
    )
  })
  return { nodes: [focus, ...near, ...far], edges: edges.slice(0, 300), hubs: {} }
}

export const capFocus = relation('table', 'hub')
