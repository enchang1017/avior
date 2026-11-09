import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VTwinkles from '../VTwinkles.vue'

describe('VTwinkles', () => {
  it('draws a few twinkling stars that nothing can point at or read', () => {
    const sky = mount(VTwinkles)
    expect(sky.attributes('aria-hidden')).toBe('true')
    expect(sky.classes()).toContain('pointer-events-none')
    const stars = sky.findAll('.star')
    expect(stars.length).toBeGreaterThan(0)
    expect(stars.length).toBeLessThanOrEqual(8)
    for (const star of stars) expect(star.classes()).toContain('animate-twinkle')
  })
})
