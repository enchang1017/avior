import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ResultGrid from './ResultGrid.vue'
import type { QueryResult } from '@/shared/api'

function result(overrides: Partial<QueryResult> = {}): QueryResult {
  return {
    columns: [
      { name: 'member_id', type: 'integer' },
      { name: 'login_name', type: 'text' },
    ],
    rows: [
      [100001, 'alice'],
      [100002, 'bob'],
      [100003, null],
    ],
    rowCount: 3,
    truncated: false,
    elapsedMs: 7,
    auditId: '00000000-0000-0000-0000-0000000000ff',
    limits: { maxCost: 50, maxRows: 1000000 },
    ...overrides,
  }
}

describe('ResultGrid', () => {
  it('renders the columns with their types and every row', () => {
    const wrapper = mount(ResultGrid, { props: { result: result() } })

    expect(wrapper.findAll('thead th')).toHaveLength(3)
    expect(wrapper.find('thead').text()).toContain('text')
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(wrapper.text()).toContain('3 rows')
  })

  // A blank cell and a cell holding the string "NULL" have to look different, or
  // an operator cannot tell an absent value from a present one.
  it('renders a null distinctly', () => {
    const wrapper = mount(ResultGrid, { props: { result: result() } })
    const nulls = wrapper.findAll('td.nul')

    expect(nulls).toHaveLength(1)
    expect(nulls[0]?.text()).toBe('NULL')
  })

  it('renders an object as JSON rather than as [object Object]', () => {
    const wrapper = mount(ResultGrid, {
      props: { result: result({ rows: [[1, { a: 2 }]], rowCount: 1 }) },
    })

    expect(wrapper.text()).toContain('{"a":2}')
    expect(wrapper.text()).not.toContain('[object Object]')
  })

  it('filters rows and reports how many of how many are shown', async () => {
    const wrapper = mount(ResultGrid, { props: { result: result() } })

    await wrapper.find('input[type="search"]').setValue('alice')

    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.text()).toContain('1 of 3')
  })

  it('matches a filter against any column', async () => {
    const wrapper = mount(ResultGrid, { props: { result: result() } })

    await wrapper.find('input[type="search"]').setValue('100002')

    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.text()).toContain('bob')
  })

  it('says so when the filter matches nothing', async () => {
    const wrapper = mount(ResultGrid, { props: { result: result() } })

    await wrapper.find('input[type="search"]').setValue('zzz')

    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    expect(wrapper.text()).toContain('No row matches')
  })

  // Truncation means the result is not the answer, only the start of one.
  it('marks a truncated result', () => {
    const wrapper = mount(ResultGrid, { props: { result: result({ truncated: true }) } })

    expect(wrapper.find('.chip').text()).toBe('truncated')
    expect(wrapper.find('.chip').classes()).toContain('text-danger')
  })

  // ADR 0009 P17: a long result costs what a screen of it costs.
  it('keeps at most sixty rows of a long result in the DOM', () => {
    const rows = Array.from({ length: 500 }, (_, index) => [index, `name-${index}`])
    const wrapper = mount(ResultGrid, { props: { result: result({ rows, rowCount: 500 }) } })

    expect(wrapper.findAll('tbody tr[data-row]').length).toBeLessThanOrEqual(60)
    expect(wrapper.find('tbody tr.spacer').exists()).toBe(true)
    expect(wrapper.text()).toContain('500')
  })

  describe('export', () => {
    let written = ''

    beforeEach(() => {
      written = ''
      vi.stubGlobal(
        'Blob',
        class {
          constructor(parts: string[]) {
            written = parts.join('')
          }
        },
      )
      vi.stubGlobal('URL', {
        createObjectURL: () => 'blob:stub',
        revokeObjectURL: () => undefined,
      })
    })

    it('writes a header row and every value quoted', async () => {
      const wrapper = mount(ResultGrid, { props: { result: result() } })

      await wrapper.get('button').trigger('click')

      const lines = written.split(String.fromCharCode(13, 10))
      expect(lines[0]).toBe('"member_id","login_name"')
      expect(lines[1]).toBe('"100001","alice"')
      expect(lines[3]).toBe('"100003","NULL"')
    })

    // An unescaped quote shifts every following column by one, and the operator
    // has no way to tell from the spreadsheet that it happened.
    it('doubles a quote inside a value', async () => {
      const wrapper = mount(ResultGrid, {
        props: { result: result({ rows: [[1, 'say "hi"']], rowCount: 1 }) },
      })

      await wrapper.get('button').trigger('click')

      expect(written).toContain('"say ""hi"""')
    })

    it('exports every row, not only the filtered ones', async () => {
      const wrapper = mount(ResultGrid, { props: { result: result() } })
      await wrapper.find('input[type="search"]').setValue('alice')

      await wrapper.get('button').trigger('click')

      expect(written.split(String.fromCharCode(13, 10))).toHaveLength(4)
    })
  })
})
