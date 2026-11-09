import { describe, expect, it } from 'vitest'

import {
  buildScene,
  touchesColumn,
  describeNeighbourhood,
  dynamicRoutines,
  edgeCaption,
  maxDrawnEdges,
  maxDrawnNodes,
  nodeLabel,
  toneOf,
  withExpansion,
} from './scene'
import type { LineageEdge, LineageGraph, LineageNode } from './types'

const node = (id: string, hop: number, extra: Partial<LineageNode> = {}): LineageNode => ({
  id,
  kind: 'table',
  schema: 'public',
  name: id,
  hop,
  ...extra,
})
const edge = (from: string, to: string, extra: Partial<LineageEdge> = {}): LineageEdge => ({
  from,
  to,
  kind: 'routine_write',
  access: 'write',
  confidence: 'parsed',
  ...extra,
})

const placed = (ids: string[]) =>
  new Map(ids.map((id, index) => [id, { x: index * 50 - 100, y: index * 10 }]))

describe('toneOf', () => {
  it('never colours a foreign key as a write, cascade or not', () => {
    const key = edge('member', 'bet', { kind: 'foreign_key', confidence: 'exact' })
    expect(toneOf({ ...key, access: 'write' })).toBe('reference')
    expect(toneOf({ ...key, access: 'reference' })).toBe('reference')
    expect(toneOf(edge('settle', 'bet'))).toBe('write')
    expect(toneOf(edge('settle', 'bet', { kind: 'routine_read', access: 'read' }))).toBe('read')
  })
})

describe('buildScene', () => {
  it('dims everything off the chosen route, the way a focus dims beyond its first hop', () => {
    const a = edge('from', 'mid', { kind: 'view_read', access: 'read' })
    const b = edge('mid', 'to', { kind: 'view_read', access: 'read' })
    const other = edge('from', 'aside', { kind: 'view_read', access: 'read' })
    const ids = ['from', 'mid', 'to', 'aside']
    const scene = buildScene({
      nodes: ids.map((id): LineageNode => ({ id, kind: 'table', schema: 'public', name: id })),
      edges: [a, b, other],
      placement: placed(ids),
      rings: [],
      focus: 'from',
      route: [a, b],
    })
    expect(scene.stars.map((star) => [star.id, star.tier])).toEqual([
      ['from', 'focus'],
      ['mid', 'near'],
      ['to', 'near'],
      ['aside', 'far'],
    ])
    expect(scene.edges.map((line) => [line.to, line.far])).toEqual([
      ['mid', false],
      ['to', false],
      ['aside', true],
    ])
  })

  it('labels a routine by its name and keeps the signature for the tooltip', () => {
    const routine = node('r', 1, { kind: 'function', name: 'place_round', arguments: 'p integer' })
    const scene = buildScene({
      nodes: [routine],
      edges: [],
      placement: placed(['r']),
      rings: [],
      focus: null,
    })
    expect(scene.stars[0]).toMatchObject({
      label: 'place_round',
      title: 'public.place_round(p integer)',
    })
  })

  it('draws the focus, the first hop in full and the rest far', () => {
    const scene = buildScene({
      nodes: [node('bet', 0), node('member', 1), node('ledger', 2)],
      edges: [],
      placement: placed(['bet', 'member', 'ledger']),
      rings: [130],
      focus: 'bet',
    })
    expect(scene.stars.map((star) => [star.id, star.tier])).toEqual([
      ['bet', 'focus'],
      ['member', 'near'],
      ['ledger', 'far'],
    ])
    expect(scene.ring).toEqual({ x: -100, y: 0 })
  })

  it('colours read and write and nothing else, and dashes what was parsed', () => {
    const scene = buildScene({
      nodes: [node('a', 0), node('b', 1), node('c', 1), node('d', 1)],
      edges: [
        edge('b', 'a'),
        edge('c', 'a', { kind: 'view_read', access: 'read', confidence: 'exact' }),
        edge('d', 'a', { kind: 'foreign_key', access: 'reference', confidence: 'exact' }),
        edge('a', 'd', { kind: 'trigger', access: 'reference', confidence: 'exact' }),
      ],
      placement: placed(['a', 'b', 'c', 'd']),
      rings: [],
      focus: 'a',
    })
    expect(scene.edges.map((line) => [line.tone, line.parsed, line.mark])).toEqual([
      ['write', true, null],
      ['read', false, null],
      ['reference', false, 'foreign_key'],
      ['reference', false, 'trigger'],
    ])
  })

  it('draws each edge once, and none to a star it did not place', () => {
    const scene = buildScene({
      nodes: [node('a', 0), node('b', 1)],
      edges: [edge('b', 'a'), edge('b', 'a'), edge('b', 'ghost'), edge('a', 'a')],
      placement: placed(['a', 'b']),
      rings: [],
      focus: 'a',
    })
    expect(scene.edges).toHaveLength(1)
  })

  it('marks the chosen route edge by edge, in order', () => {
    const first = edge('a', 'b')
    const second = edge('b', 'c', { kind: 'foreign_key', access: 'reference' })
    const scene = buildScene({
      nodes: [node('a', 0), node('b', 1), node('c', 1)],
      edges: [second, first, edge('a', 'c')],
      placement: placed(['a', 'b', 'c']),
      rings: [],
      focus: 'a',
      route: [first, second],
    })
    expect(scene.edges.map((line) => line.step)).toEqual([1, 0, null])
  })

  it('carries a routine dynamic badge, and its callers may-reach-unknown, onto the star', () => {
    const scene = buildScene({
      nodes: [
        node('purge', 1, { kind: 'function', badges: ['dynamic', 'search_path_assumed'] }),
        node('close', 1, { kind: 'procedure', badges: ['may_reach_unknown'] }),
        node('bet', 0),
      ],
      edges: [edge('purge', 'bet', { kind: 'routine_read', access: 'read' })],
      placement: placed(['purge', 'close', 'bet']),
      rings: [],
      focus: 'bet',
    })
    expect(scene.stars.map((star) => star.badge)).toEqual(['dynamic', 'may_reach_unknown', null])
    expect(scene.edges).toHaveLength(1)
  })

  it('never draws more than the cap', () => {
    const ids = Array.from({ length: 200 }, (_, index) => `n${index}`)
    const nodes = ids.map((id, index) => node(id, index === 0 ? 0 : 1))
    const edges = ids.flatMap((id) => ids.slice(0, 3).map((other) => edge(id, other)))
    const scene = buildScene({ nodes, edges, placement: placed(ids), rings: [], focus: 'n0' })
    expect(scene.stars.length).toBeLessThanOrEqual(maxDrawnNodes)
    expect(scene.edges.length).toBeLessThanOrEqual(maxDrawnEdges)
  })

  it('labels a collapsed hub even when its ring is too crowded to label', () => {
    const ids = Array.from({ length: 60 }, (_, index) => `n${index}`)
    const nodes = ids.map((id, index) =>
      node(id, index === 0 ? 0 : 1, index === 5 ? { hidden: 37 } : {}),
    )
    const scene = buildScene({ nodes, edges: [], placement: placed(ids), rings: [], focus: 'n0' })
    expect(scene.stars.find((star) => star.id === 'n5')).toMatchObject({
      labelled: true,
      hidden: 37,
    })
    expect(scene.stars.find((star) => star.id === 'n6')?.labelled).toBe(false)
  })
})

describe('touchesColumn', () => {
  const status = [{ name: 'status', restricted: false }]
  it('finds the edges that read or write the open column of the focus', () => {
    expect(touchesColumn(edge('f', 'bet', { toColumns: status }), 'bet', 'status')).toBe(true)
    expect(touchesColumn(edge('bet', 'seq', { fromColumns: status }), 'bet', 'status')).toBe(true)
    expect(
      touchesColumn(
        edge('f', 'bet', { toColumns: [{ name: '*', restricted: false }] }),
        'bet',
        'status',
      ),
    ).toBe(true)
    expect(touchesColumn(edge('f', 'bet', { toColumns: status }), 'bet', 'stake')).toBe(false)
    expect(touchesColumn(edge('bet', 'x', { toColumns: status }), 'bet', 'status')).toBe(false)
    expect(touchesColumn(edge('f', 'bet'), 'bet', 'status')).toBe(false)
  })

  it('marks them in the scene', () => {
    const scene = buildScene({
      nodes: [node('bet', 0), node('f', 1)],
      edges: [
        edge('f', 'bet', { toColumns: status }),
        edge('f', 'bet', { access: 'read', kind: 'routine_read' }),
      ],
      placement: placed(['bet', 'f']),
      rings: [],
      focus: 'bet',
      column: 'status',
    })
    expect(scene.edges.map((line) => line.highlighted)).toEqual([true, false])
  })

  it('dims every edge and star off the open column, a write elsewhere included', () => {
    const scene = buildScene({
      nodes: [node('wallet', 0), node('credit', 1), node('settle', 1), node('ledger', 1)],
      edges: [
        edge('credit', 'wallet', { toColumns: [{ name: 'balance', restricted: false }] }),
        edge('settle', 'ledger'),
        edge('settle', 'wallet', { toColumns: [{ name: 'owner', restricted: false }] }),
      ],
      placement: placed(['wallet', 'credit', 'settle', 'ledger']),
      rings: [],
      focus: 'wallet',
      column: 'balance',
    })
    expect(scene.edges.map((line) => [line.key.split('|')[0], line.highlighted, line.far])).toEqual(
      [
        [expect.stringContaining('credit'), true, false],
        [expect.stringContaining('settle'), false, true],
        [expect.stringContaining('settle'), false, true],
      ],
    )
    expect(Object.fromEntries(scene.stars.map((star) => [star.id, star.tier]))).toEqual({
      wallet: 'focus',
      credit: 'near',
      settle: 'far',
      ledger: 'far',
    })
  })
})

describe('the words beside the drawing', () => {
  const graph: LineageGraph = {
    state: 'ready',
    focus: 'bet',
    nodes: [
      node('bet', 0),
      node('settle', 1, { kind: 'function', arguments: 'bigint' }),
      node('purge', 1, { kind: 'function', badges: ['dynamic'] }),
      node('view', 1, { kind: 'view' }),
    ],
    edges: [
      edge('settle', 'bet'),
      edge('settle', 'bet', { kind: 'routine_read', access: 'read' }),
      edge('view', 'bet', { kind: 'view_read', access: 'read', confidence: 'exact' }),
    ],
  }

  it('summarises the neighbourhood for a screen reader', () => {
    expect(describeNeighbourhood(graph, 1)).toBe(
      'bet: 3 objects within 1 hop, 1 writer, 2 readers, 1 routine builds SQL at run time.',
    )
    expect(describeNeighbourhood({ state: 'ready' }, 1)).toBe('Nothing is drawn.')
  })

  it('lists the routines that build SQL at run time', () => {
    expect(dynamicRoutines(graph.nodes ?? []).map((one) => one.id)).toEqual(['purge'])
  })

  it('names a routine with its arguments and an edge by how sure it is', () => {
    expect(nodeLabel(node('settle', 1, { kind: 'function', arguments: 'bigint' }))).toBe(
      'settle(bigint)',
    )
    expect(edgeCaption(edge('a', 'b'))).toBe('PARSED · routine write · write')
  })
})

describe('withExpansion', () => {
  const current: LineageGraph = {
    state: 'ready',
    focus: 'a',
    nodes: [node('a', 0), node('hub', 1, { hidden: 2 })],
    edges: [edge('hub', 'a')],
  }

  it('adds the hub neighbours once, one hop past the hub, and opens the hub', () => {
    const expanded = withExpansion(
      current,
      {
        state: 'ready',
        nodes: [node('hub', 0), node('x', 1), node('y', 1), node('a', 1)],
        edges: [edge('x', 'hub'), edge('y', 'hub'), edge('hub', 'a')],
      },
      'hub',
    )
    expect(expanded.nodes?.map((one) => [one.id, one.hop, one.hidden ?? 0])).toEqual([
      ['a', 0, 0],
      ['hub', 1, 0],
      ['x', 2, 0],
      ['y', 2, 0],
    ])
    expect(expanded.edges).toHaveLength(3)
  })

  it('holds the cap however much the hub hid', () => {
    const many = Array.from({ length: 400 }, (_, index) => node(`m${index}`, 1))
    const expanded = withExpansion(
      current,
      { state: 'ready', nodes: many, edges: many.map((one) => edge(one.id, 'hub')) },
      'hub',
    )
    expect(expanded.nodes?.length).toBe(maxDrawnNodes)
    expect(expanded.edges?.length).toBeLessThanOrEqual(maxDrawnEdges)
  })
})
