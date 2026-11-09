import { describe, expect, it } from 'vitest'

import {
  drawnPart,
  maxDrawnEdges,
  maxDrawnNodes,
  type SchemaEdge,
  type SchemaGraph,
  type SchemaNode,
} from './graph'
import { firstRing, outerRing, radialLayout, stretch } from './layout'
import { describeChart, rowsLabel, showingNote } from './summary'

const node = (name: string, schema = 'public', kind: SchemaNode['kind'] = 'table'): SchemaNode => ({
  schema,
  name,
  kind,
  columns: 3,
  approxRows: 10,
})

const edge = (from: string, to: string, kind: SchemaEdge['kind'] = 'foreign_key'): SchemaEdge => ({
  from: { schema: 'public', name: from },
  to: { schema: 'public', name: to },
  kind,
})

const graphOf = (nodes: SchemaNode[], edges: SchemaEdge[]): SchemaGraph => ({
  nodes,
  edges,
  total: { nodes: nodes.length, edges: edges.length },
  truncated: false,
})

const ref = (name: string) => ({ schema: 'public', name })

/** A hub with `spokes` neighbours, each with `leaves` of its own, plus `loose` unconnected tables. */
function galaxy(spokes: number, leaves: number, loose: number): SchemaGraph {
  const nodes = [node('hub')]
  const edges: SchemaEdge[] = []
  for (let s = 0; s < spokes; s += 1) {
    nodes.push(node(`spoke${s}`))
    edges.push(edge(`spoke${s}`, 'hub'))
    for (let l = 0; l < leaves; l += 1) {
      nodes.push(node(`leaf${s}_${l}`))
      edges.push(edge(`leaf${s}_${l}`, `spoke${s}`))
    }
  }
  for (let i = 0; i < loose; i += 1) nodes.push(node(`loose${i}`, 'zeta'))
  return graphOf(nodes, edges)
}

const radius = (x: number, y: number) => Math.hypot(x / stretch, y)

describe('what the chart draws of a graph', () => {
  it('draws everything when the graph fits', () => {
    const graph = graphOf([node('member'), node('bet')], [edge('bet', 'member')])
    const drawn = drawnPart(graph, null)
    expect(drawn.nodes).toHaveLength(2)
    expect(drawn.edges).toHaveLength(1)
    expect(drawn.total).toBe(2)
  })

  it('keeps the target, its neighbours, then second-degree ones, up to the cap', () => {
    const graph = galaxy(20, 10, 400)
    const drawn = drawnPart(graph, ref('hub'))
    const names = new Set(drawn.nodes.map((n) => n.name))
    expect(drawn.nodes).toHaveLength(maxDrawnNodes)
    expect(names.has('hub')).toBe(true)
    for (let s = 0; s < 20; s += 1) expect(names.has(`spoke${s}`)).toBe(true)
    expect([...names].some((name) => name.startsWith('loose'))).toBe(false)
    expect(drawn.total).toBe(graph.nodes.length)
  })

  it('keeps the most connected objects when nothing is targeted', () => {
    const drawn = drawnPart(galaxy(20, 10, 400), null)
    const names = drawn.nodes.map((n) => n.name)
    expect(names).toHaveLength(maxDrawnNodes)
    expect(names).toContain('hub')
    expect(names.some((name) => name.startsWith('loose'))).toBe(false)
  })

  it('ignores a target the graph does not hold', () => {
    const drawn = drawnPart(galaxy(20, 10, 400), ref('nowhere'))
    expect(drawn.nodes.map((n) => n.name)).toContain('hub')
  })

  it('never draws more edges than the cap, and keeps the target’s first', () => {
    const nodes = Array.from({ length: 40 }, (_, i) => node(`t${i}`))
    const edges: SchemaEdge[] = []
    for (let a = 0; a < 40; a += 1)
      for (let b = a + 1; b < 40; b += 1) edges.push(edge(`t${a}`, `t${b}`))
    const drawn = drawnPart(graphOf(nodes, edges), ref('t39'))
    expect(drawn.edges).toHaveLength(maxDrawnEdges)
    const touching = drawn.edges.filter((e) => e.from.name === 't39' || e.to.name === 't39')
    expect(touching).toHaveLength(39)
  })

  it('keeps the same objects for the same input', () => {
    const graph = galaxy(30, 10, 50)
    expect(drawnPart(graph, null).nodes).toEqual(drawnPart(graph, null).nodes)
  })
})

describe('the radial layout', () => {
  const graph = graphOf(
    [
      node('member'),
      node('bet'),
      node('member_summary', 'public', 'view'),
      node('ledger', 'audit'),
    ],
    [edge('bet', 'member'), edge('member_summary', 'member', 'view_dependency')],
  )

  it('puts the target in the middle and what it touches on the first ring', () => {
    const { stars, rings } = radialLayout(graph.nodes, graph.edges, ref('member'))
    const at = new Map(stars.map((s) => [s.name, s]))
    expect(at.get('member')).toMatchObject({ x: 0, y: 0, ring: 0 })
    expect(at.get('bet')?.ring).toBe(1)
    expect(at.get('member_summary')?.ring).toBe(1)
    expect(at.get('ledger')?.ring).toBe(2)
    const bet = at.get('bet')
    const ledger = at.get('ledger')
    expect(bet && radius(bet.x, bet.y)).toBeCloseTo(firstRing, 0)
    expect(ledger && radius(ledger.x, ledger.y)).toBeCloseTo(outerRing, 0)
    expect(rings).toEqual([firstRing, outerRing])
  })

  it('draws no rings and no centre without a target', () => {
    const { stars, rings } = radialLayout(graph.nodes, graph.edges, null)
    expect(rings).toEqual([])
    expect(stars.every((s) => s.ring === 2)).toBe(true)
  })

  it('draws each edge once, from its ends, and marks the ones on the target', () => {
    const doubled = [...graph.edges, edge('bet', 'member')]
    const { lines } = radialLayout(graph.nodes, doubled, ref('member'))
    expect(lines).toHaveLength(2)
    expect(lines.every((l) => l.near)).toBe(true)
    expect(lines.find((l) => l.kind === 'view_dependency')).toMatchObject({ x2: 0, y2: 0 })
  })

  it('is a pure function of the graph and the target', () => {
    const big = drawnPart(galaxy(20, 10, 400), ref('hub'))
    const once = radialLayout(big.nodes, big.edges, ref('hub'))
    const again = radialLayout([...big.nodes].reverse(), [...big.edges].reverse(), ref('hub'))
    expect(again.stars).toEqual(once.stars)
  })

  it('never stacks two stars on one point', () => {
    const big = drawnPart(galaxy(20, 10, 400), ref('hub'))
    const { stars } = radialLayout(big.nodes, big.edges, ref('hub'))
    expect(new Set(stars.map((s) => `${s.x},${s.y}`)).size).toBe(stars.length)
  })

  it('moves a crowded first ring outward rather than overlapping it', () => {
    const big = drawnPart(galaxy(149, 0, 0), ref('hub'))
    const { stars } = radialLayout(big.nodes, big.edges, ref('hub'))
    const first = stars.filter((s) => s.ring === 1)
    expect(first.length).toBeLessThan(40)
    expect(stars).toHaveLength(150)
  })

  it('labels the outer ring only when there is room', () => {
    const small = radialLayout(graph.nodes, graph.edges, ref('member'))
    expect(small.stars.every((s) => s.labelled)).toBe(true)
    const big = drawnPart(galaxy(20, 10, 400), ref('hub'))
    const crowded = radialLayout(big.nodes, big.edges, ref('hub'))
    expect(crowded.stars.filter((s) => s.ring === 2).some((s) => s.labelled)).toBe(false)
    expect(crowded.stars.filter((s) => s.ring < 2).every((s) => s.labelled)).toBe(true)
  })
})

describe('the chart in words', () => {
  it('names what the target references and what references it', () => {
    const edges = [edge('bet', 'member'), edge('member', 'tier')]
    expect(describeChart('aviorfixture', 3, edges, ref('member'))).toBe(
      'aviorfixture: 3 objects, 2 relationships. member references tier and is referenced by bet.',
    )
  })

  it('says nothing when there is nothing', () => {
    expect(describeChart('aviorfixture', 3, [edge('bet', 'member')], ref('member'))).toBe(
      'aviorfixture: 3 objects, 1 relationship. member references nothing and is referenced by bet.',
    )
    expect(describeChart('x', 1, [], ref('member'))).toContain('referenced by nothing')
    expect(describeChart('x', 1, [], null)).toBe('x: 1 object, 0 relationships.')
  })

  it('lists several names in order', () => {
    const edges = [edge('bet', 'member'), edge('audit', 'member'), edge('card', 'member')]
    expect(describeChart('x', 4, edges, ref('member'))).toContain('by audit, bet and card')
  })

  it('says how many are shown only when some are not', () => {
    expect(showingNote(150, 612)).toBe('showing 150 of 612')
    expect(showingNote(3, 3)).toBe('')
  })

  it('prints an approximate row count, and nothing for one never analysed', () => {
    expect(rowsLabel(1_250_000)).toBe('1.25M')
    expect(rowsLabel(84_000)).toBe('84K')
    expect(rowsLabel(null)).toBe('')
    expect(rowsLabel(-1)).toBe('')
  })
})
