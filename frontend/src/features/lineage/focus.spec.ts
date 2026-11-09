import { describe, expect, it } from 'vitest'

import { names, refKey, refLabel, refOf, refQuery } from './focus'
import type { LineageNode } from './types'

const node = (id: string, extra: Partial<LineageNode> = {}): LineageNode => ({
  id,
  kind: 'table',
  schema: 'public',
  name: id,
  ...extra,
})

const settle = node('f1', { kind: 'function', name: 'settle', arguments: 'p_bet bigint' })
const settleTwo = node('f2', {
  kind: 'function',
  name: 'settle',
  arguments: 'p_bet bigint, p_at date',
})

describe('refOf', () => {
  it('names a relation by schema, name and kind', () => {
    expect(refOf(node('bet'))).toEqual({ schema: 'public', name: 'bet', kind: 'table' })
    expect(refOf(node('v', { kind: 'matview' })).kind).toBe('matview')
  })

  it('names a routine by name alone unless another known routine shares it', () => {
    expect(refOf(settle, [settle])).toEqual({ schema: 'public', name: 'settle', kind: 'routine' })
    expect(refOf(settle, [settle, settleTwo])).toEqual({
      schema: 'public',
      name: 'settle',
      kind: 'routine',
      signature: 'p_bet bigint',
    })
    const none = node('f3', { kind: 'procedure', name: 'settle' })
    expect(refOf(none, [none, settle]).signature).toBe('')
    expect(refOf(settle, [node('settle')])).not.toHaveProperty('signature')
  })
})

describe('names', () => {
  it('matches the node a reference names, the signature when there is one', () => {
    expect(names({ schema: 'public', name: 'bet', kind: 'table' }, node('bet'))).toBe(true)
    expect(names({ schema: 'public', name: 'bet', kind: 'view' }, node('bet'))).toBe(false)
    expect(names({ schema: 'public', name: 'settle', kind: 'routine' }, settle)).toBe(true)
    const exact = {
      schema: 'public',
      name: 'settle',
      kind: 'routine' as const,
      signature: 'p_bet bigint',
    }
    expect(names(exact, settle)).toBe(true)
    expect(names(exact, settleTwo)).toBe(false)
  })
})

describe('the words and keys of a reference', () => {
  it('keeps two overloads apart and says which is which', () => {
    const one = refOf(settle, [settle, settleTwo])
    const two = refOf(settleTwo, [settle, settleTwo])
    expect(refKey(one)).not.toBe(refKey(two))
    expect(refLabel(one)).toBe('public.settle(p_bet bigint) · routine')
    expect(refLabel({ schema: 'public', name: 'Bet Ledger', kind: 'table' })).toBe(
      'public.Bet Ledger',
    )
  })

  it('asks by name, unquoted, with a prefix for either end of a path', () => {
    expect(refQuery({ schema: 'public', name: 'Bet Ledger', kind: 'table' })).toEqual({
      schema: 'public',
      name: 'Bet Ledger',
      kind: 'table',
    })
    expect(refQuery({ schema: 's', name: 'f', kind: 'routine', signature: '' }, 'from')).toEqual({
      fromSchema: 's',
      fromName: 'f',
      fromKind: 'routine',
      fromSignature: '',
    })
  })
})
