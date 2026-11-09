import { describe, expect, it } from 'vitest'

import { particlesFor } from './particles'
import type { ChartEdge } from './types'

const edge = (key: string, step: number | null = null): ChartEdge => ({
  key,
  from: 'a',
  to: 'b',
  x1: -500,
  y1: -320,
  x2: 500,
  y2: 320,
  tone: 'write',
  parsed: false,
  mark: null,
  far: false,
  step,
  highlighted: false,
  caption: '',
})

describe('particlesFor', () => {
  it('puts six on a hovered edge, spread over one journey, in screen pixels', () => {
    const found = particlesFor([edge('x')], edge('x'), 0.5)
    expect(found).toHaveLength(6)
    expect(found[0]).toMatchObject({ from: '0px, 0px', to: '500px, 320px', offset: '0' })
    expect(new Set(found.map((particle) => particle.offset)).size).toBe(6)
  })

  it('shares six along the chosen route and never more', () => {
    const route = [edge('c', 2), edge('a', 0), edge('b', 1), edge('free')]
    const found = particlesFor(route, null, 1)
    expect(found).toHaveLength(6)
    expect(found.map((particle) => particle.key.split('#')[0])).toEqual([
      'a',
      'a',
      'b',
      'b',
      'c',
      'c',
    ])
    const long = Array.from({ length: 9 }, (_, index) => edge(`e${index}`, index))
    expect(particlesFor(long, null, 1)).toHaveLength(6)
  })

  it('draws none with nothing hovered and no route', () => {
    expect(particlesFor([edge('x')], null, 1)).toEqual([])
  })
})
