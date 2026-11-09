import { describe, expect, it } from 'vitest'

import { sceneHeight, sceneWidth, stretch } from '@/components/Data/VLineageChart/types'
import { radialLayout } from './layout'
import { pathLayout } from './pathLayout'
import type { LineageEdge, LineageNode } from './types'

const node = (id: string, hop: number, kind: LineageNode['kind'] = 'table'): LineageNode => ({
  id,
  kind,
  schema: 'public',
  name: id,
  hop,
})
const edge = (from: string, to: string): LineageEdge => ({
  from,
  to,
  kind: 'foreign_key',
  access: 'reference',
  confidence: 'exact',
})

function neighbourhood(near: number, perNear: number) {
  const nodes = [node('focus', 0)]
  const edges: LineageEdge[] = []
  for (let n = 0; n < near; n += 1) {
    nodes.push(node(`n${n}`, 1, n % 3 === 0 ? 'function' : 'table'))
    edges.push(edge(`n${n}`, 'focus'))
    for (let f = 0; f < perNear; f += 1) {
      nodes.push(node(`f${n}_${f}`, 2))
      edges.push(edge(`f${n}_${f}`, `n${n}`))
    }
  }
  return { nodes, edges }
}

function at(placement: Map<string, { x: number; y: number }>, id: string) {
  const point = placement.get(id)
  if (point === undefined) throw new Error(`${id} was not placed`)
  return point
}

const distance = (point: { x: number; y: number }) => Math.hypot(point.x / stretch, point.y)

describe('radialLayout', () => {
  it('puts the focus at the centre and every other star on a ring', () => {
    const { nodes, edges } = neighbourhood(6, 2)
    const { placement, rings } = radialLayout(nodes, edges, 'focus')
    expect(placement.get('focus')).toEqual({ x: 0, y: 0 })
    expect(placement.size).toBe(nodes.length)
    expect(rings.length).toBeGreaterThanOrEqual(2)
  })

  it('keeps the first hop inside everything further out', () => {
    const { nodes, edges } = neighbourhood(20, 5)
    const { placement } = radialLayout(nodes, edges, 'focus')
    const near = nodes.filter((one) => one.hop === 1).map((one) => distance(at(placement, one.id)))
    const far = nodes.filter((one) => one.hop === 2).map((one) => distance(at(placement, one.id)))
    expect(Math.max(...near)).toBeLessThan(Math.min(...far))
  })

  it('places a far star behind the first-hop star it touches', () => {
    const { nodes, edges } = neighbourhood(4, 1)
    const { placement } = radialLayout(nodes, edges, 'focus')
    for (let n = 0; n < 4; n += 1) {
      const near = at(placement, `n${n}`)
      const far = at(placement, `f${n}_0`)
      const angle = (p: { x: number; y: number }) => Math.atan2(p.y * stretch, p.x)
      const gap = Math.abs(
        Math.atan2(Math.sin(angle(near) - angle(far)), Math.cos(angle(near) - angle(far))),
      )
      expect(gap).toBeLessThan(Math.PI / 4)
    }
  })

  it('fits 150 stars inside the drawing and never on top of one another', () => {
    const { nodes, edges } = neighbourhood(29, 4)
    expect(nodes).toHaveLength(146)
    const { placement } = radialLayout(nodes, edges, 'focus')
    const points = [...placement.values()]
    for (const point of points) {
      expect(Math.abs(point.x)).toBeLessThanOrEqual(sceneWidth / 2)
      expect(Math.abs(point.y)).toBeLessThanOrEqual(sceneHeight / 2)
    }
    expect(new Set(points.map((point) => `${point.x},${point.y}`)).size).toBe(points.length)
  })

  it('draws the same answer the same way, whatever order it came in', () => {
    const { nodes, edges } = neighbourhood(8, 3)
    const once = radialLayout(nodes, edges, 'focus').placement
    const again = radialLayout([...nodes].reverse(), [...edges].reverse(), 'focus').placement
    expect([...again].sort()).toEqual([...once].sort())
  })

  it('treats an unmarked neighbour of the focus as its first hop', () => {
    const unmarked: LineageNode = { id: 'a', kind: 'table', schema: 'public', name: 'a' }
    const nodes = [node('focus', 0), unmarked]
    const { placement, rings } = radialLayout(nodes, [edge('a', 'focus')], 'focus')
    expect(distance(at(placement, 'a'))).toBeCloseTo(rings[0] ?? 0, 0)
  })

  it('fills the scene less the room its outer labels need, at one hop and at two', () => {
    for (const [near, perNear] of [
      [8, 0],
      [8, 3],
    ] as const) {
      const { nodes, edges } = neighbourhood(near, perNear)
      const { placement } = radialLayout(nodes, edges, 'focus')
      const points = [...placement.values()]
      const wide = Math.max(...points.map((point) => Math.abs(point.x)))
      const tall = Math.max(...points.map((point) => Math.abs(point.y)))
      expect(wide).toBeLessThanOrEqual(sceneWidth / 2 - 150 + 0.1)
      expect(tall).toBeLessThanOrEqual(sceneHeight / 2 - 30 + 0.1)
      const filled = Math.max(wide / (sceneWidth / 2 - 150), tall / (sceneHeight / 2 - 30))
      expect(filled, `${near} near, ${perNear} each`).toBeGreaterThan(0.99)
    }
  })
})

describe('pathLayout', () => {
  it('puts the two ends left and right and each route on a row', () => {
    const placement = pathLayout(
      [
        { nodes: ['a', 'm', 'b'], edges: [] },
        { nodes: ['a', 'x', 'y', 'b'], edges: [] },
      ],
      'a',
      'b',
    )
    expect(placement.get('a')).toEqual({ x: -400, y: 0 })
    expect(placement.get('b')).toEqual({ x: 400, y: 0 })
    expect(placement.get('m')).toEqual({ x: 0, y: -55 })
    expect(placement.get('x')?.y).toBe(55)
    expect(at(placement, 'x').x).toBeLessThan(at(placement, 'y').x)
  })

  it('keeps a star two routes share where the first put it', () => {
    const placement = pathLayout(
      [
        { nodes: ['a', 'm', 'b'], edges: [] },
        { nodes: ['a', 'q', 'm', 'b'], edges: [] },
      ],
      'a',
      'b',
    )
    expect(placement.get('m')).toEqual({ x: 0, y: -55 })
  })
})
