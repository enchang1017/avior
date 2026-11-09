import { adjacency, keyOf, type NodeRef, type SchemaEdge, type SchemaNode } from './graph'

export const chartWidth = 400
export const chartHeight = 240
/** Rings are drawn as ellipses, wider than tall, to fill a panel that is. */
export const stretch = 1.6
export const firstRing = 48
export const outerRing = 84
const ringStep = 18
const starSpacing = 14
const labelledOuterLimit = 40

export type Star = {
  key: string
  schema: string
  name: string
  kind: SchemaNode['kind']
  columns: number
  approxRows: number | null
  x: number
  y: number
  ring: 0 | 1 | 2
  labelled: boolean
}

export type Line = {
  key: string
  from: string
  to: string
  kind: SchemaEdge['kind']
  x1: number
  y1: number
  x2: number
  y2: number
  near: boolean
}

export type Layout = { stars: Star[]; lines: Line[]; rings: number[] }

type Point = { x: number; y: number }

const tenth = (value: number) => Math.round(value * 10) / 10

function onRing(radius: number, angle: number): Point {
  return { x: tenth(Math.cos(angle) * radius * stretch), y: tenth(Math.sin(angle) * radius) }
}

function capacity(radius: number): number {
  return Math.max(1, Math.floor((Math.PI * (1 + stretch) * radius) / starSpacing))
}

function compare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

function bySchemaThenName(a: SchemaNode, b: SchemaNode): number {
  return compare(a.schema, b.schema) || compare(a.name, b.name)
}

function spread(count: number, radius: number): Point[] {
  return Array.from({ length: count }, (_, index) =>
    onRing(radius, -Math.PI / 2 + (2 * Math.PI * index) / count),
  )
}

/** Outer stars in schema arcs: one empty slot between schemas, filled ring by ring. */
function outerSlots(nodes: SchemaNode[], start: number): Point[] {
  const slots: (SchemaNode | null)[] = []
  nodes.forEach((node, index) => {
    if (index > 0 && nodes[index - 1]?.schema !== node.schema) slots.push(null)
    slots.push(node)
  })
  const points: Point[] = []
  let radius = start
  let offset = 0
  while (offset < slots.length) {
    const size = Math.min(capacity(radius), slots.length - offset)
    const ring = spread(size, radius)
    ring.forEach((point, index) => {
      if (slots[offset + index] !== null) points.push(point)
    })
    offset += size
    radius += ringStep
  }
  return points
}

function star(node: SchemaNode, at: Point, ring: Star['ring'], labelled: boolean): Star {
  return {
    key: keyOf(node),
    schema: node.schema,
    name: node.name,
    kind: node.kind,
    columns: node.columns,
    approxRows: node.approxRows,
    x: at.x,
    y: at.y,
    ring,
    labelled,
  }
}

function linesBetween(
  stars: readonly Star[],
  edges: readonly SchemaEdge[],
  targetKey: string | null,
): Line[] {
  const placed = new Map(stars.map((entry) => [entry.key, entry]))
  const lines: Line[] = []
  const seen = new Set<string>()
  for (const edge of edges) {
    const from = placed.get(keyOf(edge.from))
    const to = placed.get(keyOf(edge.to))
    if (from === undefined || to === undefined || from === to) continue
    const key = `${from.key}>${to.key}:${edge.kind}`
    if (seen.has(key)) continue
    seen.add(key)
    const touches = from.key === targetKey || to.key === targetKey
    lines.push({
      key,
      from: from.key,
      to: to.key,
      kind: edge.kind,
      x1: from.x,
      y1: from.y,
      x2: to.x,
      y2: to.y,
      near: touches,
    })
  }
  return lines
}

/**
 * Where every star goes: the target at the centre, what it touches on the first
 * ring, everything else on outer rings grouped by schema. A pure function of the
 * graph and the target, so the same input always draws the same picture.
 */
export function radialLayout(
  nodes: readonly SchemaNode[],
  edges: readonly SchemaEdge[],
  target: NodeRef | null,
): Layout {
  const targetKey = target === null ? null : keyOf(target)
  const centre = nodes.find((node) => keyOf(node) === targetKey) ?? null
  const near =
    centre === null ? new Set<string>() : (adjacency(edges).get(keyOf(centre)) ?? new Set())
  const sorted = [...nodes].sort(bySchemaThenName)
  const touching = sorted.filter((node) => near.has(keyOf(node)))
  const first = touching.slice(0, capacity(firstRing))
  const rest = [
    ...touching.slice(first.length),
    ...sorted.filter((node) => node !== centre && !near.has(keyOf(node))),
  ]
  const labelOuter = nodes.length <= labelledOuterLimit

  const stars: Star[] = []
  if (centre !== null) stars.push(star(centre, { x: 0, y: 0 }, 0, true))
  spread(first.length, firstRing).forEach((at, index) => {
    const node = first[index]
    if (node !== undefined) stars.push(star(node, at, 1, true))
  })
  const start = first.length === 0 && centre === null ? firstRing : outerRing
  outerSlots(rest, start).forEach((at, index) => {
    const node = rest[index]
    if (node !== undefined) stars.push(star(node, at, 2, labelOuter))
  })

  const lines = linesBetween(stars, edges, targetKey)
  return { stars, lines, rings: centre === null ? [] : [firstRing, outerRing] }
}
