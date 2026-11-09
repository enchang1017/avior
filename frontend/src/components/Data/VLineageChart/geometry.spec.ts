import { describe, expect, it } from 'vitest'

import { edgePath, haloPaths, magnitude, strokeWeight } from './geometry'
import type { ChartEdge } from './types'

const edge = (
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  extra: Partial<ChartEdge> = {},
): ChartEdge => ({
  key: `${x1},${y1}>${x2},${y2}`,
  from: 'a',
  to: 'b',
  x1,
  y1,
  x2,
  y2,
  tone: 'read',
  parsed: false,
  mark: null,
  far: false,
  step: null,
  highlighted: false,
  caption: '',
  ...extra,
})

const numbers = (path: string) => path.match(/-?\d+(\.\d+)?/gu)?.map(Number) ?? []

describe('edgePath', () => {
  it('keeps a route straight, with a vertex at its middle for the mark', () => {
    expect(edgePath(edge(0, 100, 200, 100), true)).toBe('M0 100 L100 100 L200 100')
  })

  it('bows an arc away from the centre, through a vertex at its middle', () => {
    const path = edgePath(edge(0, 100, 200, 100), false)
    expect(path.startsWith('M0 100 Q')).toBe(true)
    const [, , , , topX, topY] = numbers(path)
    expect(topX).toBe(100)
    expect(topY).toBeGreaterThan(100)
    const above = numbers(edgePath(edge(0, -100, 200, -100), false))[5] ?? 0
    expect(above).toBeLessThan(-100)
  })
})

describe('strokeWeight', () => {
  it('draws exact heavier than parsed, and a write a step heavier than a read', () => {
    const weights = [
      strokeWeight({ parsed: false, tone: 'write' }),
      strokeWeight({ parsed: false, tone: 'read' }),
      strokeWeight({ parsed: true, tone: 'write' }),
      strokeWeight({ parsed: true, tone: 'read' }),
    ]
    expect(weights).toEqual([2.25, 1.75, 1.75, 1.25])
  })
})

describe('magnitude', () => {
  it('grows with degree and stays within its bounds', () => {
    expect(magnitude(0)).toBe(0.8)
    expect(magnitude(3)).toBeGreaterThan(magnitude(1))
    expect(magnitude(10_000)).toBe(1.6)
  })
})

describe('haloPaths', () => {
  it('joins every edge of a tone into one path', () => {
    const halos = haloPaths(
      [edge(0, 0, 10, 0), edge(0, 10, 10, 10, { tone: 'write' }), edge(0, 20, 10, 20)],
      (one) => edgePath(one, true),
    )
    expect(halos.read.match(/M/gu)).toHaveLength(2)
    expect(halos.write.match(/M/gu)).toHaveLength(1)
    expect(halos.reference).toBe('')
  })
})
