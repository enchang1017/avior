import { sceneHeight, sceneWidth, stretch } from '@/components/Data/VLineageChart/types'
import type { LineageEdge, LineageNode, LineageNodeKind } from './types'

export type Point = { x: number; y: number }
export type Placement = Map<string, Point>

const nearRing = 130
const nearStep = 40
const farRing = 226
const farStep = 32
const nearSpacing = 30
const farSpacing = 16
/** Kept clear at the sides for the outer ring's labels, and at the top and bottom for a line. */
const labelRoom = 150
const lineRoom = 30

const kindOrder: Record<LineageNodeKind, number> = {
  table: 0,
  view: 1,
  matview: 2,
  sequence: 3,
  function: 4,
  procedure: 5,
}

const tenth = (value: number) => Math.round(value * 10) / 10
const compare = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0)

function byKindThenName(a: LineageNode, b: LineageNode): number {
  return (
    kindOrder[a.kind] - kindOrder[b.kind] ||
    compare(a.schema, b.schema) ||
    compare(a.name, b.name) ||
    compare(a.id, b.id)
  )
}

function onRing(radius: number, angle: number): Point {
  return { x: tenth(Math.cos(angle) * radius * stretch), y: tenth(Math.sin(angle) * radius) }
}

function capacity(radius: number, spacing: number): number {
  return Math.max(1, Math.floor((Math.PI * (1 + stretch) * radius) / spacing))
}

/** Stars spread evenly over as many rings as they need, in the order given. */
function rings(
  count: number,
  first: number,
  step: number,
  spacing: number,
  phase: number,
): { points: Point[]; radii: number[] } {
  const points: Point[] = []
  const radii: number[] = []
  let radius = first
  let placed = 0
  while (placed < count) {
    const size = Math.min(capacity(radius, spacing), count - placed)
    for (let index = 0; index < size; index += 1) {
      points.push(onRing(radius, phase + (2 * Math.PI * index) / size))
    }
    radii.push(radius)
    placed += size
    radius += step
  }
  return { points, radii }
}

function adjacency(edges: readonly LineageEdge[]): Map<string, Set<string>> {
  const around = new Map<string, Set<string>>()
  const link = (a: string, b: string) => {
    const set = around.get(a) ?? new Set<string>()
    set.add(b)
    around.set(a, set)
  }
  for (const edge of edges) {
    if (edge.from === edge.to) continue
    link(edge.from, edge.to)
    link(edge.to, edge.from)
  }
  return around
}

const normalised = (angle: number) => {
  const turn = 2 * Math.PI
  return ((angle % turn) + turn) % turn
}

/** The mean direction of a set of angles, so a far star sits behind what it touches. */
function meanAngle(angles: number[]): number | null {
  if (angles.length === 0) return null
  const x = angles.reduce((sum, angle) => sum + Math.cos(angle), 0)
  const y = angles.reduce((sum, angle) => sum + Math.sin(angle), 0)
  return x === 0 && y === 0 ? (angles[0] ?? null) : normalised(Math.atan2(y, x))
}

export type Radial = { placement: Placement; rings: number[] }

/**
 * Where every star of a neighbourhood goes: the focus at the centre, its first
 * hop on the inner rings grouped by kind, and everything further out behind
 * the first-hop stars it touches. One pass and two sorts, no simulation, so the
 * same answer always draws the same picture.
 */
export function radialLayout(
  nodes: readonly LineageNode[],
  edges: readonly LineageEdge[],
  focus: string | null,
): Radial {
  const around = adjacency(edges)
  const touching = focus === null ? new Set<string>() : (around.get(focus) ?? new Set<string>())
  const centre = nodes.find((node) => node.id === focus) ?? null
  const isNear = (node: LineageNode) =>
    node !== centre && (node.hop === 1 || (node.hop === undefined && touching.has(node.id)))
  const near = nodes.filter(isNear).sort(byKindThenName)
  const far = nodes.filter((node) => node !== centre && !isNear(node))

  const placement: Placement = new Map()
  if (centre !== null) placement.set(centre.id, { x: 0, y: 0 })
  const inner = rings(near.length, nearRing, nearStep, nearSpacing, -Math.PI / 2)
  const angles = new Map<string, number>()
  near.forEach((node, index) => {
    const point = inner.points[index]
    if (point === undefined) return
    placement.set(node.id, point)
    angles.set(node.id, normalised(Math.atan2(point.y * stretch, point.x)))
  })

  const fallback = (index: number) => (2 * Math.PI * index) / Math.max(1, far.length)
  const ordered = [...far].sort(byKindThenName).map((node, index) => {
    const behind = [...(around.get(node.id) ?? [])].flatMap((id) => {
      const angle = angles.get(id)
      return angle === undefined ? [] : [angle]
    })
    return { node, angle: meanAngle(behind) ?? fallback(index) }
  })
  ordered.sort((a, b) => a.angle - b.angle || byKindThenName(a.node, b.node))
  const phase = ordered[0]?.angle ?? 0
  const start =
    near.length === 0 ? nearRing : Math.max(farRing, (inner.radii.at(-1) ?? 0) + farStep)
  const outer = rings(ordered.length, start, farStep, farSpacing, phase)
  ordered.forEach((entry, index) => {
    const point = outer.points[index]
    if (point !== undefined) placement.set(entry.node.id, point)
  })

  const shown = centre === null ? [] : [...inner.radii, ...outer.radii.slice(0, 1)]
  return fitted(placement, shown)
}

/**
 * The layout grown or shrunk to fill the scene, less room for labels, so one
 * hop is not drawn in half the pane and two do not run off its edges. One
 * factor for both axes keeps the rings' shape.
 */
function fitted(placement: Placement, shown: number[]): Radial {
  let wide = 0
  let tall = 0
  for (const point of placement.values()) {
    wide = Math.max(wide, Math.abs(point.x))
    tall = Math.max(tall, Math.abs(point.y))
  }
  if (wide === 0 && tall === 0) return { placement, rings: shown }
  const factor = Math.min(
    wide === 0 ? Infinity : (sceneWidth / 2 - labelRoom) / wide,
    tall === 0 ? Infinity : (sceneHeight / 2 - lineRoom) / tall,
  )
  const scaled: Placement = new Map()
  for (const [id, point] of placement) {
    scaled.set(id, { x: tenth(point.x * factor), y: tenth(point.y * factor) })
  }
  return { placement: scaled, rings: shown.map((radius) => tenth(radius * factor)) }
}
