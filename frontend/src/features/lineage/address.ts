import type { LocationQuery, LocationQueryRaw, RouteLocationRaw } from 'vue-router'

import { isFocusKind, refQuery } from './focus'
import type { FocusRef } from './types'

export const lineageRoute = 'lineage'

export type Hops = 1 | 2 | 3

/** A neighbourhood around a focus, the routes between two objects, or the galaxy of schemas. */
export type Mode = 'neighbourhood' | 'path' | 'overview'

const modes: readonly Mode[] = ['neighbourhood', 'path', 'overview']

export type Address = {
  database: string | null
  focus: FocusRef | null
  hops: Hops
  mode: Mode
  to: FocusRef | null
  column: string | null
}

const text = (query: LocationQuery, key: string): string | null => {
  const value = query[key]
  return typeof value === 'string' && value !== '' ? value : null
}

function refFrom(query: LocationQuery, prefix: '' | 'to'): FocusRef | null {
  const key = (field: string) =>
    prefix === '' ? field : `${prefix}${field.charAt(0).toUpperCase()}${field.slice(1)}`
  const schema = text(query, key('schema'))
  const name = text(query, key('name'))
  const kind = text(query, key('kind')) ?? 'table'
  if (schema === null || name === null || !isFocusKind(kind)) return null
  const signature = query[key('signature')]
  return typeof signature === 'string' && kind === 'routine'
    ? { schema, name, kind, signature }
    : { schema, name, kind }
}

/** The page's state, read from its address. Anything unreadable is left at its default. */
export function readAddress(query: LocationQuery): Address {
  const hops = Number(text(query, 'hops') ?? '1')
  return {
    database: text(query, 'database'),
    focus: refFrom(query, ''),
    hops: hops === 2 || hops === 3 ? hops : 1,
    mode: modes.find((mode) => mode === text(query, 'mode')) ?? 'neighbourhood',
    to: refFrom(query, 'to'),
    column: text(query, 'column'),
  }
}

/** The address that holds a state, with every default left out. */
export function writeAddress(address: Address): LocationQueryRaw {
  const query: LocationQueryRaw = {}
  if (address.database !== null) query['database'] = address.database
  if (address.focus !== null) Object.assign(query, refQuery(address.focus))
  if (address.hops !== 1) query['hops'] = String(address.hops)
  if (address.mode !== 'neighbourhood') query['mode'] = address.mode
  if (address.mode === 'path' && address.to !== null)
    Object.assign(query, refQuery(address.to, 'to'))
  if (address.column !== null) query['column'] = address.column
  return query
}

/** Where "Open in Lineage" goes for a table or view somebody is looking at elsewhere. */
export function lineageLocation(
  database: string,
  target: { schema: string; name: string; kind?: 'table' | 'view' | 'partitioned' } | null,
): RouteLocationRaw {
  // Lineage knows a partitioned table as a table: that is what reads and writes it.
  const focus: FocusRef | null =
    target === null
      ? null
      : {
          schema: target.schema,
          name: target.name,
          kind: target.kind === 'view' ? 'view' : 'table',
        }
  const query: LocationQueryRaw = { database, ...(focus === null ? {} : refQuery(focus)) }
  return { name: lineageRoute, query }
}
