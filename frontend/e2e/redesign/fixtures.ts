import type { Page, Request, Route } from '@playwright/test'

import type { components } from '@/gen/api'

type Schemas = components['schemas']

export const limits: Schemas['GuardLimits'] = { maxCost: 10_000, maxRows: 100_000 }

export function verdict(
  estimatedCost: number | null,
  extra: Partial<Schemas['GuardVerdict']> = {},
): Schemas['GuardVerdict'] {
  return {
    decision: 'allow',
    reasons: [],
    referencedTables: ['public.member'],
    referencedColumns: ['public.member.id', 'public.member.username'],
    estimatedCost,
    estimatedRows: estimatedCost === null ? null : 3,
    limits,
    ...extra,
  }
}

export function result(estimatedCost: number): Schemas['QueryResult'] {
  return {
    columns: [
      { name: 'id', type: 'int8' },
      { name: 'username', type: 'text' },
    ],
    rows: [
      [1, 'alice'],
      [2, 'bob'],
    ],
    rowCount: 2,
    truncated: false,
    elapsedMs: 8,
    auditId: '00000000-0000-0000-0000-0000000000a9',
    estimatedCost,
    estimatedRows: 2,
    limits,
  }
}

const node = (
  name: string,
  kind: Schemas['SchemaNode']['kind'] = 'table',
  schema = 'public',
): Schemas['SchemaNode'] => ({
  schema,
  name,
  kind,
  columns: 4,
  approxRows: kind === 'view' ? null : 84_000,
})

const edge = (
  from: string,
  to: string,
  kind: Schemas['SchemaEdge']['kind'] = 'foreign_key',
): Schemas['SchemaEdge'] => ({
  from: { schema: 'public', name: from },
  to: { schema: 'public', name: to },
  kind,
})

export const smallGraph: Schemas['SchemaGraph'] = {
  nodes: [
    { ...node('member'), approxRows: 1_250_000 },
    node('bet'),
    node('member_summary', 'view'),
  ],
  edges: [edge('bet', 'member'), edge('member_summary', 'member', 'view_dependency')],
  total: { nodes: 3, edges: 2 },
  truncated: false,
}

/** 612 objects: member at the hub with 30 neighbours, each with 5 of their own, and the rest loose. */
export function largeGraph(): Schemas['SchemaGraph'] {
  const nodes = [node('member'), node('bet'), node('member_summary', 'view')]
  const edges = [edge('bet', 'member'), edge('member_summary', 'member', 'view_dependency')]
  for (let s = 0; s < 30; s += 1) {
    nodes.push(node(`spoke_${s}`))
    edges.push(edge(`spoke_${s}`, 'member'))
    for (let l = 0; l < 5; l += 1) {
      nodes.push(node(`leaf_${s}_${l}`))
      edges.push(edge(`leaf_${s}_${l}`, `spoke_${s}`))
    }
  }
  while (nodes.length < 612) nodes.push(node(`loose_${nodes.length}`, 'table', 'archive'))
  return { nodes, edges, total: { nodes: 612, edges: edges.length }, truncated: false }
}

export const weekStart = Date.parse('2026-09-18T09:00:00Z')

export function histogram(): Schemas['AuditHistogram'] {
  const hour = 3_600_000
  return {
    from: new Date(weekStart).toISOString(),
    to: new Date(weekStart + 168 * hour).toISOString(),
    bucketSeconds: 3600,
    buckets: Array.from({ length: 168 }, (_, i) => ({
      start: new Date(weekStart + i * hour).toISOString(),
      allowed: (i * 7) % 11,
      denied: i % 13 === 0 ? 3 : 0,
    })),
  }
}

export type Recorded = { method: string; path: string; query: URLSearchParams; body: unknown }

/** Every API request the page sends, in order. */
export function record(page: Page): Recorded[] {
  const seen: Recorded[] = []
  page.on('request', (request: Request) => {
    const url = new URL(request.url())
    if (!url.pathname.startsWith('/api/v1/')) return
    seen.push({
      method: request.method(),
      path: url.pathname.replace(/^\/api\/v1/u, ''),
      query: url.searchParams,
      body: request.postDataJSON() as unknown,
    })
  })
  return seen
}

export const calls = (seen: Recorded[], method: string, path: string) =>
  seen.filter((call) => call.method === method && call.path === path)

export async function answer(
  page: Page,
  path: string,
  handler: (route: Route) => Promise<void> | void,
): Promise<void> {
  await page.route(`**/api/v1${path}**`, async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname !== `/api/v1${path}`) return route.fallback()
    return handler(route)
  })
}

export async function answerJson(
  page: Page,
  path: string,
  body: unknown,
  status = 200,
): Promise<void> {
  await answer(page, path, (route) => route.fulfill({ status, json: body }))
}
