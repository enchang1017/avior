import { describe, expect, it } from 'vitest'

import { accessFor, openTab, tabsFor } from './access'
import { objectRows, qualifiedName } from './lastUsed'
import { auditLink, topRefusals, windowRange, type UsageRow } from './usage'
import type { ObservatoryObject } from './text'

const ids = (permissions: string[]) => tabsFor(accessFor(permissions)).map((tab) => tab.id)

describe('who sees which tab', () => {
  // FEAT-071: the owner opened the observatory to every signed-in operator on 2026-09-27.
  it.each([
    ['an administrator', ['audit.read.all']],
    ['a DBA', ['audit.read.own', 'instance.read']],
    ['a developer', ['audit.read.own', 'query.inspect']],
    ['a release approver', ['release.approve']],
  ])('gives %s every tab and everybody’s usage', (_who, permissions) => {
    expect(accessFor(permissions).usage).toBe('all')
    expect(ids(permissions)).toEqual([
      'overview',
      'usage',
      'capacity',
      'last-used',
      'performance',
      'routines',
    ])
  })

  it('keeps the audit log each caller may open, for where a usage row links', () => {
    expect(accessFor(['audit.read.all']).auditLog).toBe('all')
    expect(accessFor(['audit.read.own']).auditLog).toBe('own')
    expect(accessFor(['release.approve']).auditLog).toBeNull()
  })

  it('opens the tab asked for when it is offered, and Overview otherwise', () => {
    const tabs = tabsFor(accessFor(['audit.read.own']))
    expect(openTab('performance', tabs)).toBe('performance')
    expect(openTab('routines', tabs)).toBe('routines')
    expect(openTab(undefined, tabs)).toBe('overview')
    expect(openTab('nonsense', tabs)).toBe('overview')
    expect(openTab('usage', [])).toBe('')
  })
})

const row: UsageRow = {
  operatorId: '00000000-0000-0000-0000-000000000001',
  operator: 'lin.wei',
  database: 'aviorfixture',
  statements: 12,
  allowed: 8,
  refused: 4,
  refusals: [
    { reason: 'wildcard_select', count: 2 },
    { reason: 'column_denied', count: 1 },
    { reason: 'missing_limit', count: 1 },
    { reason: 'cost', count: 1 },
  ],
  executed: 8,
  elapsedP50Ms: 12,
  elapsedP95Ms: 90,
  rowsTotal: 400,
}

describe('usage rows', () => {
  it('names the three most frequent refusal reasons', () => {
    expect(topRefusals(row)).toBe('wildcard_select ×2, column_denied ×1, missing_limit ×1')
    expect(topRefusals({ ...row, refusals: [] })).toBe('—')
  })

  it('spans the window back from now', () => {
    const range = windowRange(7, new Date('2026-09-27T00:00:00Z'))
    expect(range).toEqual({ from: '2026-09-20T00:00:00.000Z', to: '2026-09-27T00:00:00.000Z' })
  })

  it("links to the administrator's log with the operator, to the caller's own, or nowhere", () => {
    const range = { from: 'a', to: 'b' }
    expect(auditLink(row, 'all', range, 'someone-else')).toEqual({
      name: 'audit-all',
      query: { database: 'aviorfixture', from: 'a', to: 'b', operator: 'lin.wei' },
    })
    expect(auditLink(row, 'own', range, row.operatorId)).toEqual({
      name: 'audit-mine',
      query: { database: 'aviorfixture', from: 'a', to: 'b' },
    })
    expect(auditLink(row, 'own', range, 'someone-else')).toBeNull()
    expect(auditLink(row, null, range, row.operatorId)).toBeNull()
  })
})

describe('last used filters', () => {
  const base = { schema: 'public', signature: null, flagged: false } as ObservatoryObject
  const objects = [
    { ...base, name: 'bet', kind: 'table', flagged: true },
    { ...base, name: 'bet_pkey', kind: 'index' },
    { ...base, name: 'settle', kind: 'function', signature: 'bigint', flagged: true },
  ] as ObservatoryObject[]

  it('filters by kind and by flag', () => {
    const names = (kind: string, flaggedOnly: boolean) =>
      objectRows(objects, [], kind, flaggedOnly).map((row) =>
        row.row === 'group' ? '' : row.object.name,
      )
    expect(names('', false)).toHaveLength(3)
    expect(names('table', false)).toEqual(['bet'])
    expect(names('', true)).toEqual(['bet', 'settle'])
    expect(names('index', true)).toEqual([])
  })

  it('writes a routine with its arguments, so overloads stay apart', () => {
    expect(objects.map(qualifiedName)).toEqual([
      'public.bet',
      'public.bet_pkey',
      'public.settle(bigint)',
    ])
  })
})
