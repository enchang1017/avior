import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VLogo from '../VLogo.vue'

describe('VLogo', () => {
  it('is named for a screen reader and drawn in the palette', () => {
    const logo = mount(VLogo)
    expect(logo.attributes('aria-label')).toBe('Avior')
    expect(logo.find('path').classes()).toContain('fill-target')
    expect(logo.findAll('svg')).toHaveLength(1)
  })

  it('adds the wordmark when asked', () => {
    expect(mount(VLogo, { props: { wordmark: true } }).findAll('svg')).toHaveLength(2)
  })
})
