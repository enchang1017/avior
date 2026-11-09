import { describe, expect, it, vi } from 'vitest'

import { hitItems, indexingNote, tableActions, type SearchHit } from './hits'

const hits: SearchHit[] = [
  { kind: 'operator', label: 'svc-report', id: 'o1', serviceAccount: true },
  {
    kind: 'table',
    label: 'public.member',
    database: 'aviorfixture',
    schema: 'public',
    name: 'member',
    objectKind: 'table',
  },
  { kind: 'database', label: 'aviorfixture', database: 'aviorfixture' },
  {
    kind: 'table',
    label: 'public.member_summary',
    database: 'aviorfixture',
    schema: 'public',
    name: 'member_summary',
    objectKind: 'view',
  },
  {
    kind: 'write_request',
    label: 'restore member tier',
    id: 'w1',
    database: 'aviorfixture',
    state: 'pending',
  },
  { kind: 'release', label: 'r1', id: 'r1', database: 'aviorfixture', state: 'applied' },
  { kind: 'release', label: 'r2', id: 'r2', database: 'aviorfixture', state: 'pending' },
  { kind: 'operator', label: 'chen.yu', id: 'o2', serviceAccount: false },
]

describe('the server’s hits as palette items', () => {
  it('groups by kind in a fixed order and keeps the server’s ranking inside each', () => {
    const items = hitItems(
      hits,
      () => undefined,
      () => true,
    )
    expect(items.map((item) => item.group)).toEqual([
      'Databases',
      'Tables',
      'Tables',
      'Update requests',
      'Releases',
      'Releases',
      'Operators',
      'Operators',
    ])
    expect(items.slice(1, 3).map((item) => [item.label, item.hint, item.icon])).toEqual([
      ['public.member', 'aviorfixture · table', 'table'],
      ['public.member_summary', 'aviorfixture · view', 'eye'],
    ])
    expect(new Set(items.map((item) => item.id)).size).toBe(items.length)
  })

  it('leads each hit to the page that shows it', () => {
    const go = vi.fn()
    hitItems(hits, go, () => true).forEach((item) => item.run())
    expect(go.mock.calls.map(([to]) => to as unknown)).toEqual([
      { name: 'query-console', query: { database: 'aviorfixture' } },
      {
        name: 'query-console',
        query: { database: 'aviorfixture', schema: 'public', table: 'member', kind: 'table' },
      },
      {
        name: 'query-console',
        query: {
          database: 'aviorfixture',
          schema: 'public',
          table: 'member_summary',
          kind: 'view',
        },
      },
      { name: 'write-requests', query: { open: 'w1' } },
      { name: 'release-detail', params: { id: 'r1' } },
      { name: 'release-detail', params: { id: 'r2' } },
      { name: 'admin-service-accounts', query: { focus: 'svc-report' } },
      { name: 'admin-accounts', query: { focus: 'chen.yu' } },
    ])
  })

  it('says how many databases are still being indexed', () => {
    expect(indexingNote({ hits: [], limit: 20, indexing: 0 })).toBe('')
    expect(indexingNote({ hits: [], limit: 20, indexing: 1 })).toBe('still indexing 1 database')
    expect(indexingNote({ hits: [], limit: 20, indexing: 2 })).toBe('still indexing 2 databases')
  })
})

describe('a table row’s destinations', () => {
  const table: SearchHit = {
    kind: 'table',
    label: 'bet',
    database: 'aviorfixture',
    schema: 'public',
    name: 'bet',
    objectKind: 'table',
  }

  it('offers query, object and lineage, each named for where it goes', () => {
    const go = vi.fn()
    const actions = tableActions(table, go, () => true)
    expect(actions.map((one) => [one.label, one.describe])).toEqual([
      ['Query', 'Open public.bet in the query console'],
      ['Object', 'Open public.bet in its object viewer'],
      ['Lineage', 'Open public.bet in Lineage'],
    ])
    actions.forEach((one) => one.run())
    expect(go.mock.calls.map(([to]) => to as unknown)).toEqual([
      {
        name: 'query-console',
        query: { database: 'aviorfixture', schema: 'public', table: 'bet', kind: 'table' },
      },
      { name: 'objects-table', query: { database: 'aviorfixture', schema: 'public', name: 'bet' } },
      {
        name: 'lineage',
        query: { database: 'aviorfixture', schema: 'public', name: 'bet', kind: 'table' },
      },
    ])
  })

  it('sends a view to the view viewer and to lineage as a view', () => {
    const go = vi.fn()
    tableActions({ ...table, objectKind: 'view' }, go, () => true).forEach((one) => one.run())
    expect(go.mock.calls.slice(1).map(([to]) => to as unknown)).toEqual([
      { name: 'objects-view', query: { database: 'aviorfixture', schema: 'public', name: 'bet' } },
      {
        name: 'lineage',
        query: { database: 'aviorfixture', schema: 'public', name: 'bet', kind: 'view' },
      },
    ])
  })

  it('leaves out an action whose page the caller may not open, and keeps the row’s primary first', () => {
    const go = vi.fn()
    const noLineage = (route: string) => route !== 'lineage'
    expect(tableActions(table, go, noLineage).map((one) => one.label)).toEqual(['Query', 'Object'])
    const [row] = hitItems([table], go, (route) => route === 'lineage')
    expect(row?.actions).toBeUndefined()
    row?.run()
    expect(go).toHaveBeenLastCalledWith({
      name: 'lineage',
      query: { database: 'aviorfixture', schema: 'public', name: 'bet', kind: 'table' },
    })
  })
})
