import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VField from '../VField.vue'

describe('VField', () => {
  // The label has to point at the control, or clicking it does nothing and a
  // screen reader reads an input with no name.
  it('ties its label to the control it labels', () => {
    const field = mount(VField, {
      props: { label: 'Database', for: 'release-database' },
      slots: { default: '<input id="release-database" />' },
    })

    expect(field.find('label').attributes('for')).toBe('release-database')
    expect(field.find('label').text()).toBe('Database')
  })

  // A filter stretched across the window reads as the page's main input, which
  // it is not. Only the ones holding a qualified name get the extra room.
  it('is narrow unless it holds a qualified name', () => {
    const narrow = mount(VField, { props: { label: 'State', for: 'a' } }).classes()
    expect(narrow).toEqual(expect.arrayContaining(['basis-48', 'shrink-0']))

    const wide = mount(VField, { props: { label: 'Table', for: 'b', wide: true } }).classes()
    expect(wide).toEqual(expect.arrayContaining(['basis-88', 'shrink']))
    expect(wide).not.toContain('basis-48')
  })
})
