import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VLoader from '../VLoader.vue'

describe('VLoader', () => {
  it.each([
    ['inline', 'size-7'],
    ['section', 'size-16'],
    ['page', 'size-40'],
  ] as const)('draws the %s size at its own measure', (size, measure) => {
    const loader = mount(VLoader, { props: { size, label: 'Reading the lineage' } })
    expect(loader.attributes('data-size')).toBe(size)
    expect(loader.get('.loader-orb').classes()).toContain(measure)
  })

  it('is a section loader unless told otherwise', () => {
    const loader = mount(VLoader, { props: { label: 'Reading' } })
    expect(loader.attributes('data-size')).toBe('section')
    expect(loader.get('.loader-orb').classes()).toContain('size-16')
  })

  it.each(['inline', 'section', 'page'] as const)(
    'announces itself politely by its label at the %s size',
    (size) => {
      const loader = mount(VLoader, { props: { size, label: 'Reading the lineage' } })
      expect(loader.attributes('role')).toBe('status')
      expect(loader.attributes('aria-live')).toBe('polite')
      expect(loader.attributes('aria-label')).toBe('Reading the lineage')
      expect(loader.get('.loader-orb').attributes('aria-hidden')).toBe('true')
    },
  )

  it('shows its label as text only inline', () => {
    const inline = mount(VLoader, { props: { size: 'inline', label: 'Loading tables' } })
    expect(inline.text()).toBe('Loading tables')
    const section = mount(VLoader, { props: { size: 'section', label: 'Loading tables' } })
    expect(section.text()).toBe('')
  })

  it('lets the inline text be replaced by a slot', () => {
    const loader = mount(VLoader, {
      props: { size: 'inline', label: 'Loading tables' },
      slots: { default: 'Loading 12 tables' },
    })
    expect(loader.text()).toBe('Loading 12 tables')
    expect(loader.attributes('aria-label')).toBe('Loading tables')
  })

  it('moves only the comet, through the motion token', () => {
    const loader = mount(VLoader, { props: { label: 'Reading' } })
    expect(loader.findAll('[class*="animate-"]').map((node) => node.classes())).toEqual([
      expect.arrayContaining(['comet', 'animate-comet']),
    ])
  })
})
