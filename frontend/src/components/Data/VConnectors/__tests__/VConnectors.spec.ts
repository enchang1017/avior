import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VConnectors from '../VConnectors.vue'

describe('VConnectors', () => {
  it('draws each line as one elbow that turns halfway between its ends', () => {
    const wrapper = mount(VConnectors, {
      props: { lines: [{ key: 'a>b', x1: 0, y1: 10, x2: 100, y2: 50 }] },
    })
    expect(wrapper.find('[data-line="a>b"]').attributes('d')).toBe('M0 10 H50 V50 H100')
  })

  it('dashes the weaker relation and only that one', () => {
    const wrapper = mount(VConnectors, {
      props: {
        lines: [
          { key: 'solid', x1: 0, y1: 0, x2: 10, y2: 0 },
          { key: 'weak', x1: 0, y1: 0, x2: 10, y2: 0, dashed: true },
        ],
      },
    })
    expect(wrapper.find('[data-line="solid"]').attributes('stroke-dasharray')).toBe('none')
    expect(wrapper.find('[data-line="weak"]').attributes('stroke-dasharray')).toBe('4 3')
  })

  it('takes no pointer and is hidden from assistive technology', () => {
    const svg = mount(VConnectors, { props: { lines: [] } }).find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.classes()).toContain('pointer-events-none')
  })

  it('turns in the lane it is given rather than halfway', () => {
    const wrapper = mount(VConnectors, {
      props: { lines: [{ key: 'a>b', x1: 0, y1: 10, x2: 100, y2: 50, turn: 30 }] },
    })
    expect(wrapper.find('[data-line="a>b"]').attributes('d')).toBe('M0 10 H30 V50 H100')
  })

  it('draws a lit line heavier and a dimmed one in the faint rule', () => {
    const wrapper = mount(VConnectors, {
      props: {
        lines: [
          { key: 'lit', x1: 0, y1: 0, x2: 10, y2: 0, emphasis: 'lit' },
          { key: 'dim', x1: 0, y1: 0, x2: 10, y2: 0, emphasis: 'dim' },
        ],
      },
    })
    expect(wrapper.find('[data-line="lit"]').attributes('stroke-width')).toBe('2')
    expect(wrapper.find('[data-line="lit"]').classes()).toContain('stroke-fg-soft')
    expect(wrapper.find('[data-line="dim"]').classes()).toContain('stroke-line')
  })
})
