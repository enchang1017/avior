import { describe, expect, it, vi } from 'vitest'

import { navigation, visibleTo } from '@/layouts/navigation'
import { matchPages, pageItems, pagesOf, rank } from './pages'

describe('ranking', () => {
  it('ranks exact, then prefix, then word prefix, then substring', () => {
    expect(rank('member', 'member')).toBe(0)
    expect(rank('member_summary', 'member')).toBe(1)
    expect(rank('public.member', 'member')).toBe(2)
    expect(rank('Admin › Log', 'log')).toBe(2)
    expect(rank('remembered', 'member')).toBe(3)
    expect(rank('bet', 'member')).toBe(-1)
    expect(rank('bet', '')).toBe(-1)
    expect(rank('Query', 'q')).toBe(1)
  })
})

describe('pages', () => {
  const developer = { roles: [], permissions: ['query.inspect', 'audit.read.own'], features: [] }

  it('lists only the pages the rail offers this caller', () => {
    const labels = pagesOf(visibleTo(navigation, developer)).map((page) => page.label)
    expect(labels).toContain('Query')
    expect(labels.some((label) => label.includes('Account Management'))).toBe(false)
    expect(labels.some((label) => label.includes('Update Requests'))).toBe(false)
  })

  it('names a child page by its group', () => {
    const admin = { roles: ['administrator'], permissions: ['audit.read.all'], features: [] }
    expect(pagesOf(visibleTo(navigation, admin)).map((page) => page.label)).toContain('Admin › Log')
  })

  it('matches from the first letter, best first, and caps the list', () => {
    const pages = Array.from({ length: 12 }, (_, i) => ({
      label: `Page ${i}`,
      route: `p${i}`,
      icon: 'file' as const,
    }))
    expect(matchPages(pages, 'page')).toHaveLength(8)
    expect(
      matchPages([{ label: 'Query', route: 'q', icon: 'terminal' }], 'q').map((p) => p.label),
    ).toEqual(['Query'])
    expect(matchPages(pages, ' ')).toEqual([])
    expect(matchPages(pages, 'zzz')).toEqual([])
  })

  it('opens the page it names', () => {
    const go = vi.fn()
    const [item] = pageItems(
      [{ label: 'Query', route: 'query-console', icon: 'terminal' }],
      'qu',
      go,
    )
    expect(item).toMatchObject({
      id: 'page:query-console',
      group: 'Pages',
      icon: 'terminal',
      hint: 'page',
    })
    item?.run()
    expect(go).toHaveBeenCalledWith({ name: 'query-console' })
  })
})
