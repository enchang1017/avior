import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VTable from '../VTable.vue'
import { tableVariants } from '../variants'

const rows = {
  head: '<th scope="col">Name</th><th scope="col">Rows</th>',
  default: '<tr><td>member</td><td>12</td></tr><tr><td>bet</td><td>3</td></tr>',
}

describe('VTable', () => {
  it('puts the header cells in one head row and the rows in the body', () => {
    const table = mount(VTable, { slots: rows })

    expect(table.findAll('thead tr')).toHaveLength(1)
    expect(table.findAll('thead th').map((cell) => cell.text())).toEqual(['Name', 'Rows'])
    expect(table.findAll('tbody tr')).toHaveLength(2)
  })

  it('draws no head row when there are no header cells', () => {
    expect(
      mount(VTable, { slots: { default: rows.default } })
        .find('thead')
        .exists(),
    ).toBe(false)
  })

  // A caption names the table for a screen reader even where the page does not
  // show one, so hiding it must not remove it.
  it('keeps a caption for assistive technology unless it is to be seen', () => {
    const hidden = mount(VTable, { slots: { ...rows, caption: 'Audit log' } })
    expect(hidden.find('caption').text()).toBe('Audit log')
    expect(hidden.find('caption').classes()).toContain('sr-only')

    const shown = mount(VTable, {
      props: { captionVisible: true },
      slots: { ...rows, caption: 'Checks' },
    })
    expect(shown.find('caption').classes()).not.toContain('sr-only')
  })

  it('draws the shape it is asked for', () => {
    for (const variant of Object.keys(tableVariants) as (keyof typeof tableVariants)[]) {
      const classes = mount(VTable, { props: { variant }, slots: rows }).attributes('class')
      expect(classes).toContain(tableVariants[variant])
    }
  })

  // Every cell rule carries no weight, so a cell's own utility wins over it.
  // A rule written without :where() would silently beat `text-right`.
  it('never outranks a cell of its own', () => {
    for (const classes of Object.values(tableVariants)) {
      for (const utility of classes.split(' ')) {
        if (utility.startsWith('[')) expect(utility).toMatch(/^\[:where\(&\)/u)
      }
    }
  })
})
