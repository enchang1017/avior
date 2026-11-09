import type {
  ChartEdge,
  ChartScene,
  ChartStar,
  EdgeMark,
  EdgeTone,
  Tier,
} from '@/components/Data/VLineageChart/types'
import type { Placement } from './layout'
import type { LineageEdge, LineageEdgeKind, LineageGraph, LineageNode } from './types'

export const maxDrawnNodes = 150
export const maxDrawnEdges = 300
const nearLabelLimit = 48
const farLabelLimit = 24
const starGap = 8
const parallelGap = 5

const kindWords: Record<LineageEdgeKind, string> = {
  foreign_key: 'foreign key',
  view_read: 'view read',
  trigger: 'trigger',
  policy_read: 'policy read',
  default: 'default',
  routine_read: 'routine read',
  routine_write: 'routine write',
  call: 'call',
  inheritance: 'inheritance',
}

export function nodeLabel(node: LineageNode): string {
  const routine = node.kind === 'function' || node.kind === 'procedure'
  return routine ? `${node.name}(${node.arguments ?? ''})` : node.name
}

export function qualified(node: LineageNode): string {
  return `${node.schema}.${nodeLabel(node)}`
}

export function edgeKey(edge: LineageEdge): string {
  return `${edge.from}>${edge.to}:${edge.kind}:${edge.access}`
}

function tierOf(node: LineageNode, focus: string | null): Tier {
  if (node.id === focus) return 'focus'
  return node.hop === undefined || node.hop <= 1 ? 'near' : 'far'
}

function markOf(kind: LineageEdgeKind): EdgeMark {
  return kind === 'foreign_key' || kind === 'trigger' ? kind : null
}

/**
 * Read and write are the two edge colours. A foreign key is neither, even
 * where its cascade makes it a write: its icon says what it is, and a pink key
 * would read as a routine writing the table.
 */
export function toneOf(edge: LineageEdge): EdgeTone {
  if (edge.kind === 'foreign_key') return 'reference'
  return edge.access === 'read' || edge.access === 'write' ? edge.access : 'reference'
}

export function edgeCaption(edge: LineageEdge): string {
  return `${edge.confidence.toUpperCase()} · ${kindWords[edge.kind]} · ${toneOf(edge)}`
}

/** A segment pulled back from both stars, so its arrowhead ends at the star rather than under it. */
function trimmed(from: { x: number; y: number }, to: { x: number; y: number }, shift: number) {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const length = Math.hypot(dx, dy)
  if (length <= starGap * 2) return { x1: from.x, y1: from.y, x2: to.x, y2: to.y }
  const ux = (dx / length) * starGap
  const uy = (dy / length) * starGap
  const px = (-dy / length) * shift
  const py = (dx / length) * shift
  const round = (value: number) => Math.round(value * 10) / 10
  return {
    x1: round(from.x + ux + px),
    y1: round(from.y + uy + py),
    x2: round(to.x - ux + px),
    y2: round(to.y - uy + py),
  }
}

/** Two stars joined more than once get their edges side by side, so a read never hides a write. */
function shifts(edges: readonly LineageEdge[]): Map<LineageEdge, number> {
  const pairs = new Map<string, LineageEdge[]>()
  for (const edge of edges) {
    const pair = edge.from < edge.to ? `${edge.from}|${edge.to}` : `${edge.to}|${edge.from}`
    pairs.set(pair, [...(pairs.get(pair) ?? []), edge])
  }
  const shift = new Map<LineageEdge, number>()
  for (const group of pairs.values()) {
    group.forEach((edge, index) => {
      const across = (index - (group.length - 1) / 2) * parallelGap
      shift.set(edge, edge.from < edge.to ? across : -across)
    })
  }
  return shift
}

export type SceneInput = {
  nodes: readonly LineageNode[]
  edges: readonly LineageEdge[]
  placement: Placement
  rings: number[]
  focus: string | null
  /** The chosen route's edges, in order, when a path is drawn. */
  route?: readonly LineageEdge[]
  ringAt?: string | null
  /** The column the panel is open on, whose readers and writers are drawn in amber. */
  column?: string | null
}

/** Whether an edge into or out of the focus reads or writes one of its columns. */
export function touchesColumn(edge: LineageEdge, focus: string | null, column: string): boolean {
  const names = (side: LineageEdge['toColumns']) => (side ?? []).map((one) => one.name)
  const onFocus = [
    ...(edge.to === focus ? names(edge.toColumns) : []),
    ...(edge.from === focus ? names(edge.fromColumns) : []),
  ]
  return onFocus.includes(column) || (edge.to === focus && onFocus.includes('*'))
}

/** The stars on the chosen route, or null when no route is chosen. */
function onRoute(route: readonly LineageEdge[] | undefined): Set<string> | null {
  if (route === undefined || route.length === 0) return null
  return new Set(route.flatMap((edge) => [edge.from, edge.to]))
}

/** The edges that read or write the open column, or null when no column is open. */
function columnEdges(input: SceneInput): readonly LineageEdge[] | null {
  const column = input.column
  if (column === undefined || column === null) return null
  return input.edges.filter((edge) => touchesColumn(edge, input.focus, column))
}

/**
 * What the chart draws of an answer: every placed star and each distinct edge
 * between them. With a route chosen, or a column open, everything off its
 * edges is dimmed, the way everything beyond the first hop is around a focus.
 */
export function buildScene(input: SceneInput): ChartScene {
  const placed = input.nodes.filter((node) => input.placement.has(node.id))
  const touching = columnEdges(input)
  const chosen =
    onRoute(input.route) ??
    (touching === null ? null : new Set(touching.flatMap((edge) => [edge.from, edge.to])))
  const tierFor = (node: LineageNode): Tier =>
    chosen === null || node.id === input.focus
      ? tierOf(node, input.focus)
      : chosen.has(node.id)
        ? 'near'
        : 'far'
  const tiers = new Map(placed.map((node) => [node.id, tierFor(node)]))
  const near = placed.filter((node) => tiers.get(node.id) === 'near').length
  const far = placed.length - near - (input.focus === null ? 0 : 1)

  const degree = new Map<string, number>()
  for (const edge of input.edges) {
    if (edge.from === edge.to) continue
    degree.set(edge.from, (degree.get(edge.from) ?? 0) + 1)
    degree.set(edge.to, (degree.get(edge.to) ?? 0) + 1)
  }
  const stars: ChartStar[] = placed.map((node) => {
    const at = input.placement.get(node.id) ?? { x: 0, y: 0 }
    const tier = tiers.get(node.id) ?? 'far'
    const labelled =
      tier === 'focus' || (tier === 'near' ? near <= nearLabelLimit : far <= farLabelLimit)
    return {
      id: node.id,
      shape: node.kind,
      label: node.name,
      title: qualified(node),
      x: at.x,
      y: at.y,
      tier,
      labelled: labelled || (node.hidden ?? 0) > 0,
      side: at.x < -1 ? 'left' : 'right',
      hidden: node.hidden ?? 0,
      degree: degree.get(node.id) ?? 0,
      extension: node.extension === true,
      badge: node.badges?.includes('dynamic')
        ? 'dynamic'
        : node.badges?.includes('may_reach_unknown')
          ? 'may_reach_unknown'
          : null,
    }
  })

  const steps = new Map((input.route ?? []).map((edge, index) => [edgeKey(edge), index]))
  const seen = new Set<string>()
  const distinct = input.edges.filter((edge) => {
    const key = edgeKey(edge)
    const drawable =
      input.placement.has(edge.from) && input.placement.has(edge.to) && edge.from !== edge.to
    if (!drawable || seen.has(key)) return false
    seen.add(key)
    return true
  })
  const shift = shifts(distinct)
  const edges: ChartEdge[] = []
  for (const edge of distinct) {
    const from = input.placement.get(edge.from) ?? { x: 0, y: 0 }
    const to = input.placement.get(edge.to) ?? { x: 0, y: 0 }
    const key = edgeKey(edge)
    const highlighted = touching?.includes(edge) === true
    edges.push({
      key,
      from: edge.from,
      to: edge.to,
      ...trimmed(from, to, shift.get(edge) ?? 0),
      tone: toneOf(edge),
      parsed: edge.confidence === 'parsed',
      mark: markOf(edge.kind),
      far:
        chosen === null
          ? tiers.get(edge.from) === 'far' || tiers.get(edge.to) === 'far'
          : steps.size > 0
            ? !steps.has(key)
            : !highlighted,
      step: steps.get(key) ?? null,
      highlighted,
      caption: edgeCaption(edge),
    })
    if (edges.length >= maxDrawnEdges) break
  }

  const ringAt = input.ringAt === undefined ? input.focus : input.ringAt
  const ring = ringAt === null ? null : (input.placement.get(ringAt) ?? null)
  return { stars: stars.slice(0, maxDrawnNodes), edges, rings: input.rings, ring }
}

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`

/** Routines in the answer that build SQL at run time: never drawn as edges, listed instead. */
export function dynamicRoutines(nodes: readonly LineageNode[]): LineageNode[] {
  return nodes.filter((node) => node.badges?.includes('dynamic') === true)
}

/** The drawing in words, for the screen reader that cannot see it. */
export function describeNeighbourhood(graph: LineageGraph, hops: number): string {
  const nodes = graph.nodes ?? []
  const edges = graph.edges ?? []
  const focus = nodes.find((node) => node.id === graph.focus)
  if (focus === undefined) return 'Nothing is drawn.'
  const around = nodes.length - 1
  const into = (access: 'read' | 'write') =>
    new Set(edges.filter((e) => e.to === focus.id && e.access === access).map((e) => e.from)).size
  const dynamic = dynamicRoutines(nodes).length
  return (
    `${focus.name}: ${plural(around, 'object', 'objects')} within ${plural(hops, 'hop', 'hops')}, ` +
    `${plural(into('write'), 'writer', 'writers')}, ${plural(into('read'), 'reader', 'readers')}, ` +
    `${plural(dynamic, 'routine builds', 'routines build')} SQL at run time.`
  )
}

/**
 * A collapsed hub's neighbours joined to the neighbourhood already drawn.
 * Whether the server answers the hub's neighbours alone or the whole
 * neighbourhood again, nothing is drawn twice, the hub stops being collapsed,
 * and the drawing cap still holds.
 */
export function withExpansion(
  current: LineageGraph,
  expanded: LineageGraph,
  hub: string,
): LineageGraph {
  const nodes = new Map((current.nodes ?? []).map((node) => [node.id, node]))
  const hubNode = nodes.get(hub)
  const hubHop = hubNode?.hop ?? 1
  if (hubNode !== undefined) nodes.set(hub, { ...hubNode, hidden: 0 })
  for (const node of expanded.nodes ?? []) {
    if (nodes.size >= maxDrawnNodes) break
    if (node.id === hub) continue
    if (!nodes.has(node.id))
      nodes.set(node.id, { ...node, hop: Math.max(node.hop ?? 0, hubHop + 1) })
  }
  const keys = new Set<string>()
  const edges: LineageEdge[] = []
  for (const edge of [...(current.edges ?? []), ...(expanded.edges ?? [])]) {
    const key = edgeKey(edge)
    if (keys.has(key) || !nodes.has(edge.from) || !nodes.has(edge.to)) continue
    if (edges.length >= maxDrawnEdges) break
    keys.add(key)
    edges.push(edge)
  }
  return { ...current, nodes: [...nodes.values()], edges }
}
