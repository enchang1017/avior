import type { ChartEdge, EdgeTone } from './types'

type Point = { x: number; y: number }

/** How far an arc bows from its chord, as a share of the chord's length. */
const bow = 0.12
const round = (value: number) => Math.round(value * 10) / 10
const at = (point: Point) => `${round(point.x)} ${round(point.y)}`

/**
 * An edge's path. An arc bows away from the centre, so edges curve around it
 * rather than crossing it; a route's edge stays straight, a flight line. Either
 * way the path has a vertex at its middle, where a key or trigger mark sits.
 */
export function edgePath(edge: ChartEdge, straight: boolean): string {
  const from = { x: edge.x1, y: edge.y1 }
  const to = { x: edge.x2, y: edge.y2 }
  const mid = { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 }
  if (straight) return `M${at(from)} L${at(mid)} L${at(to)}`
  const dx = to.x - from.x
  const dy = to.y - from.y
  const length = Math.hypot(dx, dy)
  if (length === 0) return `M${at(from)} L${at(to)}`
  let nx = -dy / length
  let ny = dx / length
  if (nx * mid.x + ny * mid.y < 0) {
    nx = -nx
    ny = -ny
  }
  const control = { x: mid.x + nx * length * bow, y: mid.y + ny * length * bow }
  const top = {
    x: 0.25 * from.x + 0.5 * control.x + 0.25 * to.x,
    y: 0.25 * from.y + 0.5 * control.y + 0.25 * to.y,
  }
  const first = { x: (from.x + control.x) / 2, y: (from.y + control.y) / 2 }
  const second = { x: (control.x + to.x) / 2, y: (control.y + to.y) / 2 }
  return `M${at(from)} Q${at(first)} ${at(top)} Q${at(second)} ${at(to)}`
}

/** Exact is heavier than parsed, and a write one step heavier than a read. */
export function strokeWeight(edge: Pick<ChartEdge, 'parsed' | 'tone'>): number {
  const base = edge.parsed ? 1.25 : 1.75
  return edge.tone === 'write' ? base + 0.5 : base
}

/** A star's size for how connected it is: brighter and larger, within bounds. */
export function magnitude(degree: number): number {
  return Math.min(1.6, Math.max(0.8, 0.8 + 0.25 * Math.log2(1 + degree)))
}

/** Every edge's halo as one path per tone, so a halo under each costs three elements, not three hundred. */
export function haloPaths(
  edges: readonly ChartEdge[],
  pathOf: (edge: ChartEdge) => string,
): Record<EdgeTone, string> {
  const out: Record<EdgeTone, string[]> = { read: [], write: [], reference: [] }
  for (const edge of edges) out[edge.tone].push(pathOf(edge))
  return {
    read: out.read.join(' '),
    write: out.write.join(' '),
    reference: out.reference.join(' '),
  }
}
