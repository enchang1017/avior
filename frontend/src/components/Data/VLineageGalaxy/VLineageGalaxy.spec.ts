import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import VLineageGalaxy from './VLineageGalaxy.vue'

const schemas = [
  { schema: 'public', objects: 120 },
  { schema: 'promo', objects: 12 },
  { schema: 'audit', objects: 4 },
]
const links = [{ from: 'promo', to: 'public', edges: 5 }]

afterEach(() => vi.useRealTimers())

describe('VLineageGalaxy', () => {
  it('draws a nebula per schema and a line per crossing, labelled by schema alone', () => {
    const galaxy = mount(VLineageGalaxy, { props: { schemas, links, summary: 's' } })
    expect(galaxy.findAll('[data-cluster]').map((one) => one.attributes('data-schema'))).toEqual([
      'public',
      'promo',
      'audit',
    ])
    expect(galaxy.findAll('[data-schema-link]')).toHaveLength(1)
    expect(galaxy.findAll('text').map((one) => one.text())).toEqual(['public', 'promo', 'audit'])
  })

  it('names a picked cluster', async () => {
    const galaxy = mount(VLineageGalaxy, { props: { schemas, links, summary: 's' } })
    await galaxy.find('[data-schema="promo"]').trigger('click')
    expect(galaxy.emitted('pick')).toEqual([['promo']])
  })

  it('flies by transform to the schema asked for, and says when it has landed', async () => {
    vi.useFakeTimers()
    const galaxy = mount(VLineageGalaxy, { props: { schemas, links, summary: 's' } })
    await galaxy.setProps({ flyTo: 'promo' })
    expect(galaxy.find('.camera').attributes('style')).toMatch(/transform: scale\(4\) translate\(/u)
    expect(galaxy.emitted('flown')).toBeUndefined()
    vi.runAllTimers()
    expect(galaxy.emitted('flown')).toHaveLength(1)
  })

  it('lands even when it is drawn already flying', () => {
    vi.useFakeTimers()
    const galaxy = mount(VLineageGalaxy, {
      props: { schemas, links, summary: 's', flyTo: 'audit' },
    })
    vi.runAllTimers()
    expect(galaxy.emitted('flown')).toHaveLength(1)
  })
})
