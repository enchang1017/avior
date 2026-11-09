import { describe, expect, it } from 'vitest'

import { lineageLocation, readAddress, writeAddress, type Address } from './address'

const blank: Address = {
  database: null,
  focus: null,
  hops: 1,
  mode: 'neighbourhood',
  to: null,
  column: null,
}

describe('the lineage address', () => {
  it('reads nothing as the defaults', () => {
    expect(readAddress({})).toEqual(blank)
  })

  it('reads the focus by schema, name and kind, a table when the kind is left out', () => {
    expect(readAddress({ schema: 'public', name: 'Bet Ledger', kind: 'view' }).focus).toEqual({
      schema: 'public',
      name: 'Bet Ledger',
      kind: 'view',
    })
    expect(readAddress({ schema: 'public', name: 'bet' }).focus?.kind).toBe('table')
    expect(readAddress({ schema: 'public', name: 'f', kind: 'function' }).focus).toBeNull()
    expect(readAddress({ name: 'bet' }).focus).toBeNull()
  })

  it('keeps a routine signature, an empty one included, and only for a routine', () => {
    expect(readAddress({ schema: 's', name: 'f', kind: 'routine', signature: '' }).focus).toEqual({
      schema: 's',
      name: 'f',
      kind: 'routine',
      signature: '',
    })
    expect(readAddress({ schema: 's', name: 't', signature: 'x' }).focus).not.toHaveProperty(
      'signature',
    )
  })

  it('holds hops to 1, 2 or 3', () => {
    expect(readAddress({ hops: '2' }).hops).toBe(2)
    expect(readAddress({ hops: '3' }).hops).toBe(3)
    expect(readAddress({ hops: '9' }).hops).toBe(1)
    expect(readAddress({ hops: ['2'] }).hops).toBe(1)
  })

  it('writes back what it reads, defaults left out', () => {
    const state: Address = {
      database: 'db',
      focus: { schema: 'public', name: 'bet', kind: 'table' },
      hops: 2,
      mode: 'path',
      to: { schema: 'promo', name: 'grant', kind: 'routine', signature: 'p_id bigint' },
      column: 'status',
    }
    const written = writeAddress(state)
    expect(written).toMatchObject({
      toSchema: 'promo',
      toName: 'grant',
      toSignature: 'p_id bigint',
    })
    expect(readAddress(written as Record<string, string>)).toEqual(state)
    expect(writeAddress(blank)).toEqual({})
    expect(writeAddress({ ...state, mode: 'neighbourhood' })).not.toHaveProperty('toName')
  })

  it('opens a table or view seen elsewhere as the focus, by name', () => {
    expect(lineageLocation('db', { schema: 'public', name: 'bet', kind: 'view' })).toEqual({
      name: 'lineage',
      query: { database: 'db', schema: 'public', name: 'bet', kind: 'view' },
    })
    expect(lineageLocation('db', { schema: 'public', name: 'bet' })).toMatchObject({
      query: { kind: 'table' },
    })
    expect(lineageLocation('db', null)).toEqual({ name: 'lineage', query: { database: 'db' } })
  })

  it('reads and writes the galaxy overview as a mode of its own, and nothing else as one', () => {
    expect(readAddress({ database: 'db', mode: 'overview' }).mode).toBe('overview')
    expect(readAddress({ database: 'db', mode: 'galaxy' }).mode).toBe('neighbourhood')
    expect(writeAddress({ ...readAddress({ database: 'db' }), mode: 'overview' })).toEqual({
      database: 'db',
      mode: 'overview',
    })
  })
})
