import type { RouteLocationRaw } from 'vue-router'

import type { IconName } from '@/components'
import type { components } from '@/gen/api'
import type { PaletteAction } from '@/shared/palette'
import type { SearchItem } from './pages'

export type SearchHit = components['schemas']['SearchHit']
export type SearchResult = components['schemas']['SearchResult']

const groups: Record<SearchHit['kind'], string> = {
  database: 'Databases',
  table: 'Tables',
  write_request: 'Update requests',
  release: 'Releases',
  operator: 'Operators',
}

const kindOrder: SearchHit['kind'][] = ['database', 'table', 'write_request', 'release', 'operator']

export function destination(hit: SearchHit): RouteLocationRaw {
  const database = hit.database ?? ''
  switch (hit.kind) {
    case 'database':
      return { name: 'query-console', query: { database } }
    case 'table':
      return {
        name: 'query-console',
        query: {
          database,
          schema: hit.schema ?? '',
          table: hit.name ?? '',
          kind: hit.objectKind ?? 'table',
        },
      }
    case 'write_request':
      return { name: 'write-requests', query: { open: hit.id ?? '' } }
    case 'release':
      return { name: 'release-detail', params: { id: hit.id ?? '' } }
    case 'operator':
      return {
        name: hit.serviceAccount === true ? 'admin-service-accounts' : 'admin-accounts',
        query: { focus: hit.label },
      }
  }
}

type Destination = { label: string; route: string; to: RouteLocationRaw }

/** A table's three destinations, Query first: the console is where a table has always led. */
function tableDestinations(hit: SearchHit): Destination[] {
  const database = hit.database ?? ''
  const schema = hit.schema ?? ''
  const name = hit.name ?? ''
  const view = hit.objectKind === 'view'
  return [
    { label: 'Query', route: 'query-console', to: destination(hit) },
    {
      label: 'Object',
      route: view ? 'objects-view' : 'objects-table',
      to: { name: view ? 'objects-view' : 'objects-table', query: { database, schema, name } },
    },
    {
      label: 'Lineage',
      route: 'lineage',
      to: { name: 'lineage', query: { database, schema, name, kind: view ? 'view' : 'table' } },
    },
  ]
}

const describe: Record<string, string> = {
  Query: 'in the query console',
  Object: 'in its object viewer',
  Lineage: 'in Lineage',
}

/**
 * The actions a table's row offers, leaving out any whose page the caller may
 * not open, exactly as the rail leaves it out.
 */
export function tableActions(
  hit: SearchHit,
  go: (to: RouteLocationRaw) => void,
  mayOpen: (route: string) => boolean,
): PaletteAction[] {
  return tableDestinations(hit)
    .filter((one) => mayOpen(one.route))
    .map((one) => ({
      label: one.label,
      describe: `Open ${label(hit)} ${describe[one.label] ?? ''}`.trim(),
      run: () => go(one.to),
    }))
}

function hint(hit: SearchHit): string {
  switch (hit.kind) {
    case 'database':
      return 'database'
    case 'table':
      return `${hit.database ?? ''} · ${hit.objectKind ?? 'table'}`
    case 'write_request':
    case 'release':
      return [hit.database, hit.state].filter(Boolean).join(' · ')
    case 'operator':
      return hit.serviceAccount === true ? 'service account' : 'operator'
  }
}

function icon(hit: SearchHit): IconName {
  switch (hit.kind) {
    case 'database':
      return 'database'
    case 'table':
      if (hit.objectKind === 'view') return 'eye'
      return hit.objectKind === 'partitioned' ? 'layers' : 'table'
    case 'write_request':
      return 'pull-request'
    case 'release':
      return 'release'
    case 'operator':
      return hit.serviceAccount === true ? 'bot' : 'user'
  }
}

function label(hit: SearchHit): string {
  return hit.kind === 'table' && hit.schema !== undefined && hit.name !== undefined
    ? `${hit.schema}.${hit.name}`
    : hit.label
}

/**
 * The server's hits as palette items, grouped by kind in a fixed order, each
 * keeping the server's ranking. A table's row carries its destinations; the
 * same hit gives the same row on every page.
 */
export function hitItems(
  hits: readonly SearchHit[],
  go: (to: RouteLocationRaw) => void,
  mayOpen: (route: string) => boolean,
): SearchItem[] {
  const ordered = kindOrder.flatMap((kind) => hits.filter((hit) => hit.kind === kind))
  return ordered.map((hit, index) => {
    const actions = hit.kind === 'table' ? tableActions(hit, go, mayOpen) : []
    return {
      id: `${hit.kind}:${hit.id ?? `${hit.database ?? ''}/${label(hit)}`}:${index}`,
      label: label(hit),
      hint: hint(hit),
      icon: icon(hit),
      group: groups[hit.kind],
      run: actions[0]?.run ?? (() => go(destination(hit))),
      ...(actions.length > 1 ? { actions } : {}),
    }
  })
}

export function indexingNote(result: SearchResult): string {
  const count = result.indexing
  return count === 0 ? '' : `still indexing ${count} ${count === 1 ? 'database' : 'databases'}`
}
