import { describe, expect, it } from 'vitest'

import { hopsOf } from './route'
import type { LineageEdge } from './types'

const edge = (from: string, to: string, extra: Partial<LineageEdge> = {}): LineageEdge => ({
  from,
  to,
  kind: 'view_read',
  access: 'read',
  confidence: 'exact',
  ...extra,
})

describe('hopsOf', () => {
  it('names what joins each hop, in the order the route walks it', () => {
    const hops = hopsOf({
      nodes: ['big_open_rounds', 'open_rounds', 'player'],
      edges: [edge('big_open_rounds', 'open_rounds'), edge('open_rounds', 'player')],
    })
    expect(hops.map((hop) => [hop.from, hop.to, hop.word, hop.against])).toEqual([
      ['big_open_rounds', 'open_rounds', 'reads', false],
      ['open_rounds', 'player', 'reads', false],
    ])
    expect(hops[1]?.edge?.to).toBe('player')
  })

  it('says when a hop is walked against its arrow', () => {
    const [hop] = hopsOf({
      nodes: ['round', 'place_round'],
      edges: [edge('place_round', 'round', { kind: 'routine_write', access: 'write' })],
    })
    expect(hop).toMatchObject({ from: 'round', to: 'place_round', word: 'writes', against: true })
  })

  it('words every edge kind', () => {
    const word = (extra: Partial<LineageEdge>) =>
      hopsOf({ nodes: ['a', 'b'], edges: [edge('a', 'b', extra)] })[0]?.word
    expect(word({ kind: 'foreign_key', access: 'reference' })).toBe('FK')
    expect(word({ kind: 'foreign_key', access: 'write' })).toBe('FK cascade')
    expect(word({ kind: 'trigger', access: 'reference' })).toBe('trigger')
    expect(word({ kind: 'call', access: 'reference' })).toBe('calls')
    expect(word({ kind: 'default' })).toBe('default')
    expect(word({ kind: 'inheritance', access: 'reference' })).toBe('inherits')
    expect(word({ kind: 'policy_read' })).toBe('reads')
  })
})
