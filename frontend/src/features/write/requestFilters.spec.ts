import { describe, expect, it } from 'vitest'

import { parseQuery } from '@/components'
import { matchesQuery, requestScopes, stateTabs, visibleRequests } from './requestFilters'
import type { WriteRequest } from './useWriteRequests'

function request(overrides: Partial<WriteRequest>): WriteRequest {
  const base: WriteRequest = {
    id: 'wr-1',
    database: 'aviorfixture',
    statement: 'UPDATE public.member SET tier = 1 WHERE id = 1',
    kind: 'update',
    targetTable: 'public.member',
    reason: 'Support ticket 8812',
    requestedBy: 'chen.yu',
    requestedAt: '2026-09-20T08:30:00Z',
    estimatedRows: 1,
    state: 'pending',
    reversible: true,
  }
  return { ...base, ...overrides }
}

const rows = [
  request({ id: 'a' }),
  request({ id: 'b', state: 'applied', database: 'ledger', requestedBy: 'lin.wei' }),
  request({ id: 'c', state: 'rejected', targetTable: 'public.bet', reason: 'Reset tiers' }),
]

const query = (text: string) => parseQuery(text, requestScopes)

describe('state tabs', () => {
  it('counts what was loaded, All first', () => {
    const tabs = stateTabs(rows, 'mine')
    expect(tabs[0]).toEqual({ id: '', label: 'All', count: 3 })
    expect(tabs.find((tab) => tab.id === 'pending')?.count).toBe(1)
    expect(tabs.find((tab) => tab.id === 'failed')?.count).toBe(0)
  })

  // The queue is what waits, so it has one tab and no All beside it.
  it('gives the queue one tab', () => {
    expect(stateTabs(rows, 'queue').map((tab) => tab.id)).toEqual(['pending'])
  })
})

describe('the request search', () => {
  it('narrows by scope', () => {
    expect(matchesQuery(rows[1] as WriteRequest, query('database:ledger'))).toBe(true)
    expect(matchesQuery(rows[0] as WriteRequest, query('database:ledger'))).toBe(false)
    expect(matchesQuery(rows[2] as WriteRequest, query('state:rejected'))).toBe(true)
    expect(matchesQuery(rows[0] as WriteRequest, query('state:rejected'))).toBe(false)
    expect(matchesQuery(rows[1] as WriteRequest, query('requester:LIN.WEI'))).toBe(true)
  })

  it('matches words against the table, the reason and who asked', () => {
    expect(visibleRequests(rows, '', query('reset')).map((r) => r.id)).toEqual(['c'])
    expect(visibleRequests(rows, '', query('8812 chen')).map((r) => r.id)).toEqual(['a'])
  })

  it('combines the tab with the search', () => {
    expect(visibleRequests(rows, 'pending', query('')).map((r) => r.id)).toEqual(['a'])
    expect(visibleRequests(rows, 'applied', query('database:aviorfixture'))).toEqual([])
  })
})
