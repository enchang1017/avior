import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VSkeleton from '../VSkeleton.vue'

describe('VSkeleton', () => {
  it('draws the number of blocks it was asked for', () => {
    const wrapper = mount(VSkeleton, { props: { lines: 5 } })
    expect(wrapper.findAll('.block')).toHaveLength(5)
  })

  // Announced once, on the container. Nine identical placeholders read out one
  // by one is worse than the blank page this replaces.
  it('announces itself once, not once per block', () => {
    const wrapper = mount(VSkeleton, { props: { lines: 4, label: 'Reading the release' } })
    expect(wrapper.findAll('[role="status"]')).toHaveLength(1)
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('Reading the release')
  })
})

describe('VSkeleton table', () => {
  const columns = ['w-21/25', 'w-1/2', 'w-full']

  it('draws the rows it was asked for, each with one cell per column', () => {
    const wrapper = mount(VSkeleton, { props: { variant: 'table', columns, rows: 4 } })
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(4)
    for (const row of rows) expect(row.findAll('td')).toHaveLength(columns.length)
    expect(wrapper.findAll('thead th')).toHaveLength(columns.length)
  })

  // The same table variant as the list it stands in for, so a row is the
  // height of the row that replaces it and the answer lands without a shift.
  it('draws its rows with the table variant of the list it replaces', async () => {
    const { tableVariants } = await import('../../../Data/VTable/variants')
    const wrapper = mount(VSkeleton, { props: { variant: 'table', columns, table: 'log' } })
    expect(wrapper.get('table').attributes('class')).toBe(tableVariants.log)
  })

  it('gives each bar its column width', () => {
    const wrapper = mount(VSkeleton, { props: { variant: 'table', columns, rows: 1 } })
    const bars = wrapper.findAll('tbody td span span')
    expect(bars.map((bar) => bar.classes().find((name) => name.startsWith('w-')))).toEqual(columns)
  })

  it('shows the list headings when it is given them', () => {
    const wrapper = mount(VSkeleton, {
      props: { variant: 'table', columns, headings: ['Database', 'Role', ''] },
    })
    const heads = wrapper.findAll('thead th').map((th) => th.text())
    expect(heads).toEqual(['Database', 'Role', ''])
  })

  it('announces itself once, with its label, and hides the placeholder table', () => {
    const wrapper = mount(VSkeleton, {
      props: { variant: 'table', columns, label: 'Reading the audit log' },
    })
    expect(wrapper.findAll('[role="status"]')).toHaveLength(1)
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('Reading the audit log')
    expect(wrapper.get('table').attributes('aria-hidden')).toBe('true')
  })

  // ADR 0009: one pulse per cell would be dozens of infinite animations.
  it('holds still', () => {
    const wrapper = mount(VSkeleton, { props: { variant: 'table', columns } })
    expect(wrapper.html()).not.toContain('shimmer')
    expect(wrapper.html()).not.toContain('animate-')
  })

  it('stacks the lines of a row that wraps in its first cell', () => {
    const wrapper = mount(VSkeleton, { props: { variant: 'table', columns, rows: 1, rowLines: 3 } })
    const cells = wrapper.findAll('tbody td')
    expect(cells.map((cell) => cell.findAll(':scope > span').length)).toEqual([3, 1, 1])
  })

  it('stands a row as tall as a small control when its rows hold controls', () => {
    const lines = mount(VSkeleton, { props: { variant: 'table', columns, rows: 1 } })
    const controls = mount(VSkeleton, {
      props: { variant: 'table', columns, rows: 1, controls: true },
    })
    expect(lines.get('tbody td > span').classes()).toContain('h-lh')
    expect(controls.get('tbody td > span').classes()).toContain('h-control-sm')
  })
})

describe('VSkeleton list', () => {
  it('draws the items it was asked for, each with its lines, announced once', () => {
    const wrapper = mount(VSkeleton, {
      props: { variant: 'list', rows: 3, rowLines: 4, label: 'Reading write requests' },
    })
    const rows = wrapper.findAll('[data-skeleton-row]')
    expect(rows).toHaveLength(3)
    for (const row of rows) expect(row.findAll('.block')).toHaveLength(4)
    expect(wrapper.findAll('[role="status"]')).toHaveLength(1)
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('Reading write requests')
  })
})
