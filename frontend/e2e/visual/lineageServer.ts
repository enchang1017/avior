import type { components } from '@/gen/api'

type Schemas = components['schemas']
type Node = Schemas['LineageNode']
type Edge = Schemas['LineageEdge']

/** A whole database's lineage, answered the way the server answers it: around a focus, capped. */
export type LineageWorld = {
  nodes: Node[]
  edges: Edge[]
  /** Nodes the server collapses, with the neighbours each hides. */
  hubs: Record<string, number>
}

export const snapshot: Schemas['LineageSnapshot'] = {
  builtAt: '2026-09-20T09:55:00Z',
  stale: false,
  parsed: 301,
  reused: 0,
  unparsed: 1,
  dynamic: 2,
  opaque: 0,
}

export type Reply = { status: number; body: unknown }

const refused = (status: number, code: string, message: string): Reply => ({
  status,
  body: { code, message },
})

const field = (prefix: string, name: string) =>
  prefix === '' ? name : `${prefix}${name.charAt(0).toUpperCase()}${name.slice(1)}`

/**
 * The object a request names, by id or by schema, name and kind, the way the
 * server resolves it: a routine's name alone settles it only when one routine
 * has it.
 */
export function resolve(
  world: LineageWorld,
  query: URLSearchParams,
  prefix: '' | 'from' | 'to',
): string | Reply {
  const id = query.get(prefix === '' ? 'focus' : prefix)
  const schema = query.get(field(prefix, 'schema'))
  const name = query.get(field(prefix, 'name'))
  const kind = query.get(field(prefix, 'kind'))
  const signature = query.get(field(prefix, 'signature'))
  if ((id === null) === (schema === null || name === null || kind === null)) {
    return refused(
      422,
      'lineage.focus',
      'Name the object by its id, or by its schema, name and kind.',
    )
  }
  if (id !== null) {
    return world.nodes.some((node) => node.id === id)
      ? id
      : refused(404, 'lineage.object_unknown', `${id} is not in this database.`)
  }
  const routine = kind === 'routine'
  const found = world.nodes.filter(
    (node) =>
      node.schema === schema &&
      node.name === name &&
      (routine ? node.kind === 'function' || node.kind === 'procedure' : node.kind === kind) &&
      (signature === null || (node.arguments ?? '') === signature),
  )
  const named = `${schema ?? ''}.${name ?? ''}${signature === null ? '' : `(${signature})`}`
  if (found.length === 0)
    return refused(404, 'lineage.object_unknown', `${named} is not in this database.`)
  if (found.length > 1) {
    return refused(
      422,
      'lineage.routine_ambiguous',
      `More than one routine is called ${named}. Give its signature to choose one.`,
    )
  }
  return found[0]?.id ?? ''
}

const nodeCap = 150
const edgeCap = 300

function around(world: LineageWorld): Map<string, Set<string>> {
  const links = new Map<string, Set<string>>()
  const link = (a: string, b: string) => {
    const set = links.get(a) ?? new Set<string>()
    set.add(b)
    links.set(a, set)
  }
  for (const edge of world.edges) {
    link(edge.from, edge.to)
    link(edge.to, edge.from)
  }
  return links
}

function kept(world: LineageWorld, query: URLSearchParams, focus: string) {
  const kinds = query.getAll('kinds')
  const confidence = query.getAll('confidence')
  const extensions = query.get('extensions') === 'true'
  const hidden = world.nodes.filter((node) => node.extension === true && !extensions)
  const nodes = world.nodes.filter(
    (node) =>
      !hidden.includes(node) &&
      (node.id === focus || kinds.length === 0 || kinds.includes(node.kind)),
  )
  const ids = new Set(nodes.map((node) => node.id))
  const edges = world.edges.filter(
    (edge) =>
      ids.has(edge.from) &&
      ids.has(edge.to) &&
      (confidence.length === 0 || confidence.includes(edge.confidence)),
  )
  return { world: { ...world, nodes, edges }, extensionsHidden: hidden.length }
}

/** `GET /lineage/graph`: the focus and everything within the hops, hubs folded, the cap kept. */
export function neighbourhood(whole: LineageWorld, query: URLSearchParams): Reply {
  const focus = resolve(whole, query, '')
  if (typeof focus !== 'string') return focus
  return { status: 200, body: aroundFocus(whole, query, focus) }
}

function aroundFocus(
  whole: LineageWorld,
  query: URLSearchParams,
  focus: string,
): Schemas['LineageGraph'] {
  const { world, extensionsHidden } = kept(whole, query, focus)
  const expand = query.get('expand')
  const hops = Number(query.get('hops') ?? '1')
  const byId = new Map(world.nodes.map((node) => [node.id, node]))
  const links = around(world)
  const hop = new Map<string, number>([[focus, 0]])
  let frontier = [focus]
  for (let depth = 1; depth <= hops; depth += 1) {
    const next: string[] = []
    for (const id of frontier) {
      if (id !== focus && id !== expand && world.hubs[id] !== undefined) continue
      for (const other of links.get(id) ?? []) {
        if (hop.has(other)) continue
        hop.set(other, depth)
        next.push(other)
      }
    }
    frontier = next
  }
  if (expand !== null) {
    for (const other of links.get(expand) ?? [])
      if (!hop.has(other)) hop.set(other, (hop.get(expand) ?? 1) + 1)
  }
  const ranked = [...hop.keys()]
    .filter((id) => byId.has(id))
    .sort((a, b) => (hop.get(a) ?? 0) - (hop.get(b) ?? 0) || (a < b ? -1 : 1))
  const ids = new Set(ranked.slice(0, nodeCap))
  const nodes = [...ids].map((id) => {
    const node = byId.get(id) as Node
    const folded = id !== focus && id !== expand ? world.hubs[id] : undefined
    return { ...node, hop: hop.get(id) ?? 0, ...(folded === undefined ? {} : { hidden: folded }) }
  })
  const inside = world.edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to))
  const edges = inside.slice(0, edgeCap)
  return {
    state: 'ready',
    snapshot,
    focus,
    nodes,
    edges,
    total: { nodes: hop.size, edges: inside.length },
    truncated: hop.size > nodeCap || inside.length > edgeCap,
    extensionsHidden,
    dynamic: whole.nodes.filter((node) => node.badges?.includes('dynamic') === true),
  }
}

/** `GET /lineage/path`: up to four shortest routes, within the hops asked for. */
export function routes(world: LineageWorld, query: URLSearchParams): Reply {
  const from = resolve(world, query, 'from')
  if (typeof from !== 'string') return from
  const to = resolve(world, query, 'to')
  if (typeof to !== 'string') return to
  return { status: 200, body: between(world, query, from, to) }
}

function between(
  world: LineageWorld,
  query: URLSearchParams,
  from: string,
  to: string,
): Schemas['LineagePath'] {
  const most = Number(query.get('maxHops') ?? '5')
  const links = around(world)
  const chosen: string[][] = []
  const walk = (at: string, trail: string[], length: number) => {
    if (chosen.length >= 4) return
    if (trail.length - 1 === length) {
      if (at === to) chosen.push(trail)
      return
    }
    for (const next of [...(links.get(at) ?? [])].sort()) {
      if (!trail.includes(next)) walk(next, [...trail, next], length)
    }
  }
  for (let length = 1; length <= most && chosen.length < 4; length += 1) walk(from, [from], length)
  const edgeBetween = (a: string, b: string) =>
    world.edges.find(
      (edge) => (edge.from === a && edge.to === b) || (edge.from === b && edge.to === a),
    )
  const ids = new Set(chosen.flat())
  return {
    state: 'ready',
    snapshot,
    nodes: world.nodes.filter((node) => ids.has(node.id)),
    routes: chosen.map((trail) => ({
      nodes: trail,
      edges: trail.slice(1).flatMap((id, index) => {
        const edge = edgeBetween(trail[index] ?? '', id)
        return edge === undefined ? [] : [edge]
      }),
    })),
  }
}

/** `GET /lineage/overview`: each schema a cluster anchored on its most connected object, every crossing a link. */
export function galaxy(world: LineageWorld): Reply {
  const degree = new Map<string, number>()
  for (const edge of world.edges) {
    degree.set(edge.from, (degree.get(edge.from) ?? 0) + 1)
    degree.set(edge.to, (degree.get(edge.to) ?? 0) + 1)
  }
  const schemaOf = new Map(world.nodes.map((node) => [node.id, node.schema]))
  const clusters = new Map<string, Schemas['LineageSchemaCluster']>()
  for (const node of world.nodes) {
    if (node.extension === true) continue
    const held = clusters.get(node.schema)
    const kinds = {
      tables: node.kind === 'table' ? 1 : 0,
      views: node.kind === 'view' || node.kind === 'matview' ? 1 : 0,
      routines: node.kind === 'function' || node.kind === 'procedure' ? 1 : 0,
    }
    if (held === undefined) {
      clusters.set(node.schema, { schema: node.schema, objects: 1, ...kinds, anchor: node })
      continue
    }
    const stronger = (degree.get(node.id) ?? 0) > (degree.get(held.anchor.id) ?? 0)
    clusters.set(node.schema, {
      ...held,
      objects: held.objects + 1,
      tables: held.tables + kinds.tables,
      views: held.views + kinds.views,
      routines: held.routines + kinds.routines,
      anchor: stronger ? node : held.anchor,
    })
  }
  const links = new Map<string, Schemas['LineageSchemaLink']>()
  for (const edge of world.edges) {
    const a = schemaOf.get(edge.from)
    const b = schemaOf.get(edge.to)
    if (a === undefined || b === undefined || a === b) continue
    const [from, to] = a < b ? [a, b] : [b, a]
    const held = links.get(`${from}|${to}`) ?? { from, to, edges: 0, reads: 0, writes: 0 }
    links.set(`${from}|${to}`, {
      ...held,
      edges: held.edges + 1,
      reads: held.reads + (edge.access === 'read' ? 1 : 0),
      writes: held.writes + (edge.access === 'write' && edge.kind !== 'foreign_key' ? 1 : 0),
    })
  }
  const schemas = [...clusters.values()].sort((a, b) => (a.schema < b.schema ? -1 : 1))
  return { status: 200, body: { state: 'ready', snapshot, schemas, links: [...links.values()] } }
}
