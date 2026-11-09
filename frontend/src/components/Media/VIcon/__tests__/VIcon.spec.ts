import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VIcon from '../VIcon.vue'
import { icons, isIconName } from '../icons'

describe('VIcon', () => {
  it('draws the named icon as an svg', () => {
    const svg = mount(VIcon, { props: { name: 'database' } }).find('svg')
    expect(svg.exists()).toBe(true)
    expect(svg.attributes('data-icon')).toBe('database')
    expect(svg.classes()).toContain('lucide')
  })

  // Beside a word the icon adds nothing a screen reader needs; alone, it is
  // the only thing saying what the control does.
  it('is hidden unless it is labelled', () => {
    expect(mount(VIcon, { props: { name: 'x' } }).attributes('aria-hidden')).toBe('true')
    const labelled = mount(VIcon, { props: { name: 'x', label: 'Close' } })
    expect(labelled.attributes('aria-label')).toBe('Close')
    expect(labelled.attributes('role')).toBe('img')
    expect(labelled.attributes('aria-hidden')).toBeUndefined()
  })

  it('takes its size from the scale and turns only when asked', () => {
    expect(mount(VIcon, { props: { name: 'play', size: 'lg' } }).classes()).toContain('size-5')
    expect(mount(VIcon, { props: { name: 'play' } }).classes()).not.toContain('animate-spin')
    expect(mount(VIcon, { props: { name: 'loader', spin: true } }).classes()).toContain(
      'animate-spin',
    )
  })

  it('knows its own names and no others', () => {
    expect(Object.keys(icons).length).toBeGreaterThan(40)
    expect(isIconName('release')).toBe(true)
    expect(isIconName('pi-search')).toBe(false)
  })
})
