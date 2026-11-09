import type { Placement } from './layout'
import type { LineageRoute } from './types'

const pathSpan = 400
const pathRow = 110
const tenth = (value: number) => Math.round(value * 10) / 10

/**
 * Routes between two objects, left to right: the first object on the left, the
 * second on the right, each route a row, and a star shared by two routes kept
 * where the first route put it.
 */
export function pathLayout(routes: readonly LineageRoute[], from: string, to: string): Placement {
  const placement: Placement = new Map([
    [from, { x: -pathSpan, y: 0 }],
    [to, { x: pathSpan, y: 0 }],
  ])
  routes.forEach((route, row) => {
    const y = tenth((row - (routes.length - 1) / 2) * pathRow)
    const last = Math.max(1, route.nodes.length - 1)
    route.nodes.forEach((id, index) => {
      if (placement.has(id)) return
      placement.set(id, { x: tenth(-pathSpan + (2 * pathSpan * index) / last), y })
    })
  })
  return placement
}
