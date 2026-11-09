import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VCard from '../VCard.vue'

describe('VCard', () => {
  it('is the element it is told to be', () => {
    expect(mount(VCard, { props: { as: 'section' } }).element.tagName).toBe('SECTION')
    expect(mount(VCard).element.tagName).toBe('DIV')
  })

  // A card holding a table wants its rows at the edge, so the inset is opt-in.
  it('pads itself only when asked', () => {
    expect(mount(VCard).classes()).not.toContain('p-3')
    expect(mount(VCard, { props: { pad: true } }).classes()).toContain('p-3')
  })

  it('carries a verdict down its leading edge', () => {
    const card = mount(VCard, { props: { tone: 'danger' } })
    expect(card.classes()).toEqual(expect.arrayContaining(['border-l-4', 'border-l-danger']))
    expect(mount(VCard).classes()).not.toContain('border-l-4')
  })
})
