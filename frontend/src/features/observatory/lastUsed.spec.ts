import { describe, expect, it } from 'vitest'

import { objectRows, type ObjectRow } from './lastUsed'
import type { ObservatoryObject, RoutineGroup, RoutineVersion } from './text'

function object(overrides: Partial<ObservatoryObject>): ObservatoryObject {
  return {
    schema: 'public',
    name: 'member',
    kind: 'table',
    signature: null,
    table: null,
    tracked: true,
    notTrackedReason: null,
    lastUsedAt: null,
    lastUsedKind: null,
    lastUsedNode: null,
    daysAgo: null,
    flagged: false,
    constraintBacked: false,
    sizeBytes: null,
    growth30dBytes: null,
    calls: null,
    ...overrides,
  }
}

const routine = (day: string, overrides: Partial<ObservatoryObject> = {}) =>
  object({
    name: `advance_26_09_${day}`,
    kind: 'function',
    signature: 'bigint',
    ...overrides,
  })

function version(day: string, overrides: Partial<RoutineVersion> = {}): RoutineVersion {
  return {
    name: `advance_26_09_${day}`,
    signature: 'bigint',
    date: `2026-09-${day}`,
    latest: false,
    lastUsedAt: null,
    daysAgo: null,
    flagged: false,
    ...overrides,
  }
}

const group: RoutineGroup = {
  schema: 'public',
  base: 'advance',
  latest: 'advance_26_09_25',
  flaggedCount: 1,
  lastUsedAt: null,
  versions: [version('19', { flagged: true }), version('25', { latest: true }), version('21')],
}

const objects = [
  object({ name: 'member' }),
  routine('21'),
  object({ name: 'bet', flagged: true }),
  routine('19', { flagged: true }),
  routine('25'),
  object({ name: 'settle', kind: 'procedure', signature: 'bigint' }),
]

function describeRows(rows: ObjectRow[]): string[] {
  return rows.map((row) => {
    if (row.row === 'group') return `group ${row.group.schema}.${row.group.base}`
    if (row.row === 'version') return `  ${row.object.name}${row.latest ? ' latest' : ''}`
    return row.object.name
  })
}

describe('the objects table rows', () => {
  it('draws a group where its first member stood, its versions newest first', () => {
    expect(describeRows(objectRows(objects, [group], '', false))).toEqual([
      'member',
      'group public.advance',
      '  advance_26_09_25 latest',
      '  advance_26_09_21',
      '  advance_26_09_19',
      'bet',
      'settle',
    ])
  })

  it('draws every row plainly when there is no group', () => {
    expect(describeRows(objectRows(objects, [], '', false))).toEqual(objects.map((one) => one.name))
  })

  it('keeps only the flagged versions, and the group header above them', () => {
    expect(describeRows(objectRows(objects, [group], '', true))).toEqual([
      'group public.advance',
      '  advance_26_09_19',
      'bet',
    ])
  })

  it('drops the header when no version passes the kind filter', () => {
    expect(describeRows(objectRows(objects, [group], 'table', false))).toEqual(['member', 'bet'])
    expect(describeRows(objectRows(objects, [group], 'function', false))).toEqual([
      'group public.advance',
      '  advance_26_09_25 latest',
      '  advance_26_09_21',
      '  advance_26_09_19',
    ])
  })

  it('leaves an overload with another signature out of the group', () => {
    const overload = routine('25', { signature: 'bigint, text' })
    const rows = objectRows([...objects, overload], [group], '', false)
    expect(rows.at(-1)).toEqual({
      row: 'object',
      key: 'function:public.advance_26_09_25(bigint, text)',
      object: overload,
    })
    expect(rows.filter((row) => row.row === 'version')).toHaveLength(3)
  })

  it('matches by name alone when the object carries no signature', () => {
    const rows = objectRows([routine('25', { signature: null })], [group], '', false)
    expect(rows[0]?.row).toBe('group')
    expect(rows.filter((row) => row.row === 'object')).toEqual([])
  })

  it('does not match a routine of the same name in another schema', () => {
    const elsewhere = routine('25', { schema: 'audit' })
    const rows = objectRows([elsewhere], [group], '', false)
    expect(rows[0]).toEqual({
      row: 'object',
      key: 'function:audit.advance_26_09_25(bigint)',
      object: elsewhere,
    })
  })

  it('still draws a version the object list does not carry, from the version itself', () => {
    const rows = objectRows([object({ name: 'member' })], [group], '', true)
    expect(describeRows(rows)).toEqual(['group public.advance', '  advance_26_09_19'])
    const [, only] = rows
    expect(only?.row === 'version' && only.object.flagged).toBe(true)
  })

  it('gives every row a distinct key', () => {
    const keys = objectRows(objects, [group], '', false).map((row) => row.key)
    expect(new Set(keys).size).toBe(keys.length)
  })
})
