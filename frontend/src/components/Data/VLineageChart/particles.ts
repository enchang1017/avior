import { maxParticles, sceneHeight, sceneWidth, type ChartEdge } from './types'

export type Particle = { key: string; from: string; to: string; offset: string }

function onScreen(x: number, y: number, scale: number): string {
  const px = Math.round((x + sceneWidth / 2) * scale * 10) / 10
  const py = Math.round((y + sceneHeight / 2) * scale * 10) / 10
  // A pair for translate(), which needs its comma: without one the transform
  // is invalid and every particle sits at the chart's corner.
  return `${px}px, ${py}px`
}

function along(edge: ChartEdge, count: number, scale: number): Particle[] {
  return Array.from({ length: count }, (_, index) => ({
    key: `${edge.key}#${index}`,
    from: onScreen(edge.x1, edge.y1, scale),
    to: onScreen(edge.x2, edge.y2, scale),
    offset: String(Math.round((-index / count) * 1000) / 1000),
  }))
}

/**
 * The particles showing which way data moves: along the hovered edge, or
 * shared out along the chosen route, and never more than six. They are HTML
 * moved by transform, so the compositor carries them and the drawing is never
 * repainted for them.
 */
export function particlesFor(
  edges: readonly ChartEdge[],
  hovered: ChartEdge | null,
  scale: number,
): Particle[] {
  if (hovered !== null) return along(hovered, maxParticles, scale)
  const route = edges
    .filter((edge) => edge.step !== null)
    .sort((a, b) => (a.step ?? 0) - (b.step ?? 0))
    .slice(0, maxParticles)
  if (route.length === 0) return []
  const each = Math.floor(maxParticles / route.length)
  const extra = maxParticles - each * route.length
  return route.flatMap((edge, index) => along(edge, each + (index < extra ? 1 : 0), scale))
}
