import { sceneHeight, sceneWidth } from '../VLineageChart/types'

export type SchemaInput = { schema: string; objects: number }
export type LinkInput = { from: string; to: string; edges: number }

export type Cluster = {
  schema: string
  objects: number
  x: number
  y: number
  r: number
  /** Where its name is drawn: below, or on the first side no link of its own crosses. */
  label: SchemaLabel
}

export type SchemaLabel = { x: number; y: number; anchor: 'start' | 'middle' | 'end' }

/** The measures a schema's name is placed with, in scene units. */
export const labelChar = 7
const labelLine = 14
export type GalaxyLink = {
  key: string
  x1: number
  y1: number
  x2: number
  y2: number
  width: number
}

const minRadius = 14
const maxRadius = 70
const goldenAngle = Math.PI * (3 - Math.sqrt(5))
/** Kept clear at the edges for the schemas' names. */
const margin = 60
const tenth = (value: number) => Math.round(value * 10) / 10

/** A nebula's radius for its object count: by area, so twice the objects is not twice as wide. */
export function radiusOf(objects: number, most: number): number {
  return tenth(minRadius + (maxRadius - minRadius) * Math.sqrt(objects / Math.max(1, most)))
}

function spiral(sizes: number[], spacing: number): { x: number; y: number }[] {
  return sizes.map((_, index) => {
    const reach = spacing * Math.sqrt(index)
    const angle = index * goldenAngle
    return { x: reach * Math.cos(angle), y: reach * Math.sin(angle) }
  })
}

function overlaps(points: { x: number; y: number }[], radii: number[]): boolean {
  for (let a = 0; a < points.length; a += 1)
    for (let b = a + 1; b < points.length; b += 1) {
      const gap = Math.hypot(
        (points[a]?.x ?? 0) - (points[b]?.x ?? 0),
        (points[a]?.y ?? 0) - (points[b]?.y ?? 0),
      )
      if (gap < (radii[a] ?? 0) + (radii[b] ?? 0) + 8) return true
    }
  return false
}

/**
 * Where each schema's nebula goes: the largest at the centre and the rest on a
 * golden-angle spiral, spread until no two touch, then fitted to the scene.
 * The same schemas always draw the same galaxy.
 */
export function galaxyLayout(
  schemas: readonly SchemaInput[],
  links: readonly LinkInput[],
): { clusters: Cluster[]; links: GalaxyLink[] } {
  const ordered = [...schemas].sort(
    (a, b) => b.objects - a.objects || (a.schema < b.schema ? -1 : 1),
  )
  const most = ordered[0]?.objects ?? 1
  const radii = ordered.map((one) => radiusOf(one.objects, most))
  let spacing = maxRadius
  let points = spiral(radii, spacing)
  for (let guard = 0; guard < 40 && overlaps(points, radii); guard += 1) {
    spacing *= 1.15
    points = spiral(radii, spacing)
  }
  let wide = 1
  let tall = 1
  points.forEach((point, index) => {
    wide = Math.max(wide, Math.abs(point.x) + (radii[index] ?? 0))
    tall = Math.max(tall, Math.abs(point.y) + (radii[index] ?? 0))
  })
  // Grown as well as shrunk, up to three times, so a database of two schemas
  // fills the view as one of forty does.
  const fit = Math.min(3, (sceneWidth / 2 - margin) / wide, (sceneHeight / 2 - margin) / tall)
  const placed = ordered.map((one, index) => ({
    schema: one.schema,
    objects: one.objects,
    x: tenth((points[index]?.x ?? 0) * fit),
    y: tenth((points[index]?.y ?? 0) * fit),
    r: tenth((radii[index] ?? minRadius) * fit),
  }))
  const at = new Map(placed.map((cluster) => [cluster.schema, cluster]))
  const drawn = links.flatMap((link) => {
    const a = at.get(link.from)
    const b = at.get(link.to)
    return a === undefined || b === undefined ? [] : [edgeToEdge(link, a, b)]
  })
  const clusters = placed.map((cluster) => ({
    ...cluster,
    label: labelFor(
      cluster,
      drawn.filter((link) => link.key.split('|').includes(cluster.schema)),
    ),
  }))
  return { clusters, links: drawn }
}

type Placed = Omit<Cluster, 'label'>
/** A link drawn from one nebula's edge to the other's, never across either. */
function edgeToEdge(link: LinkInput, a: Placed, b: Placed): GalaxyLink {
  const length = Math.max(1, Math.hypot(b.x - a.x, b.y - a.y))
  const ux = (b.x - a.x) / length
  const uy = (b.y - a.y) / length
  const width = tenth(1.5 + 1.5 * Math.log2(Math.max(1, link.edges)))
  return {
    key: `${link.from}|${link.to}`,
    x1: tenth(a.x + ux * a.r),
    y1: tenth(a.y + uy * a.r),
    x2: tenth(b.x - ux * b.r),
    y2: tenth(b.y - uy * b.r),
    width,
  }
}

/** The box a name takes at a place, as the chart draws it: its baseline and its anchor. */
export function labelBox(name: string, label: SchemaLabel) {
  const width = name.length * labelChar
  const left =
    label.anchor === 'start'
      ? label.x
      : label.anchor === 'end'
        ? label.x - width
        : label.x - width / 2
  return {
    left,
    right: left + width,
    top: label.y - labelLine * 0.8,
    bottom: label.y + labelLine * 0.2,
  }
}

function crosses(link: GalaxyLink, box: ReturnType<typeof labelBox>): boolean {
  for (let step = 0; step <= 40; step += 1) {
    const x = link.x1 + ((link.x2 - link.x1) * step) / 40
    const y = link.y1 + ((link.y2 - link.y1) * step) / 40
    if (x > box.left && x < box.right && y > box.top && y < box.bottom) return true
  }
  return false
}

function labelFor(cluster: Placed, own: readonly GalaxyLink[]): SchemaLabel {
  const gap = 8
  const places: SchemaLabel[] = [
    { x: cluster.x, y: cluster.y + cluster.r + gap * 2, anchor: 'middle' },
    { x: cluster.x, y: cluster.y - cluster.r - gap, anchor: 'middle' },
    { x: cluster.x + cluster.r + gap, y: cluster.y + labelLine * 0.3, anchor: 'start' },
    { x: cluster.x - cluster.r - gap, y: cluster.y + labelLine * 0.3, anchor: 'end' },
  ]
  const clear = places.find((place) =>
    own.every((link) => !crosses(link, labelBox(cluster.schema, place))),
  )
  return clear ?? (places[0] as SchemaLabel)
}
