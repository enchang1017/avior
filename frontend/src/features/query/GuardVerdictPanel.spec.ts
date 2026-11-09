import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import GuardVerdictPanel from './GuardVerdictPanel.vue'
import type { GuardVerdict } from '@/shared/api'

function render(verdict: GuardVerdict) {
  return mount(GuardVerdictPanel, { props: { verdict } })
}

describe('GuardVerdictPanel', () => {
  it('reports an allowed statement with its estimates', () => {
    const wrapper = render({
      decision: 'allow',
      reasons: [],
      referencedTables: ['public.member'],
      referencedColumns: ['public.member.member_id'],
      limits: { maxCost: 50, maxRows: 1000000 },
      estimatedCost: 0.0032,
      estimatedRows: 1,
    })

    expect(wrapper.text()).toContain('Allowed')
    expect(wrapper.text()).toContain('public.member.member_id')
    expect(wrapper.text()).toContain('0.0032')
    expect(wrapper.findAll('li')).toHaveLength(0)
  })

  // FEAT-075. Which partitions survived pruning, out of how many, is what tells
  // a statement on one month from one on all of them.
  it('names the partitions a plan scans out of all its table has', () => {
    const wrapper = render({
      decision: 'allow',
      reasons: [],
      referencedTables: ['public.rounds'],
      referencedColumns: ['public.rounds.id'],
      limits: { maxCost: 50, maxRows: 1000000 },
      estimatedCost: 12,
      estimatedRows: 1200,
      partitionScans: [
        {
          schema: 'public',
          name: 'rounds',
          total: 29,
          scanned: [
            { schema: 'public', name: 'rounds_2026_09', estimatedRows: 1200 },
            { schema: 'public', name: 'rounds_2026_10', estimatedRows: 0 },
          ],
        },
      ],
    })

    const scans = wrapper.find('.partition-scans')
    expect(scans.find('.scan-summary').text()).toMatch(
      /^public\.rounds \(partitioned\) · scanned 2 \/\s*29 partitions$/u,
    )
    expect(scans.findAll('li').map((li) => li.text())).toEqual([
      'rounds_2026_09~1,200 rows',
      'rounds_2026_10~0 rows',
    ])
  })

  it('says nothing about partitions when the plan reads none', () => {
    const wrapper = render({
      decision: 'allow',
      reasons: [],
      referencedTables: ['public.member'],
      referencedColumns: [],
      limits: { maxCost: 50, maxRows: 1000000 },
    })
    expect(wrapper.find('.partition-scans').exists()).toBe(false)
  })

  // Every rule that fired has to be listed. Rendering only the first leaves the
  // operator fixing one problem at a time without knowing how many remain.
  it('lists every reason a statement was refused', () => {
    const wrapper = render({
      decision: 'deny',
      reasons: [
        {
          rule: 'column_denied',
          message: 'not permitted',
          subject: 'public.member.id_card_number',
        },
        { rule: 'cost_limit_exceeded', message: 'estimated plan cost 58.4 exceeds the limit' },
        { rule: 'row_limit_exceeded', message: 'estimated rows 9000000 exceeds the limit' },
      ],
      referencedTables: [],
      referencedColumns: [],
      limits: { maxCost: 50, maxRows: 1000000 },
    })

    expect(wrapper.text()).toContain('Refused')
    expect(wrapper.findAll('li')).toHaveLength(3)
    expect(wrapper.text()).toContain('public.member.id_card_number')
    expect(wrapper.text()).toContain('58.4')
    expect(wrapper.text()).toContain('9000000')
  })

  it('shows a dash rather than nothing when estimates are absent', () => {
    const wrapper = render({
      decision: 'deny',
      reasons: [{ rule: 'parse_failed', message: 'unterminated string literal' }],
      referencedTables: [],
      referencedColumns: [],
      limits: { maxCost: 50, maxRows: 1000000 },
    })

    expect(wrapper.text()).toContain('—')
  })
})
