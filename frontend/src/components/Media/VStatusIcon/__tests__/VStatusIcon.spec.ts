import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VStatusIcon, { type Status } from '../VStatusIcon.vue'

describe('VStatusIcon', () => {
  // A shape and a colour for each, so neither alone has to carry it.
  it('gives every status its own shape and tone', () => {
    const drawn = (status: Status) => {
      const wrapper = mount(VStatusIcon, { props: { status } })
      return { icon: wrapper.find('svg').attributes('data-icon'), classes: wrapper.classes() }
    }
    const all: Status[] = ['ok', 'refused', 'warn', 'pending', 'running', 'idle']
    const shapes = new Set(all.map((status) => drawn(status).icon))

    expect(shapes.size).toBe(all.length)
    expect(drawn('ok').classes).toContain('text-ok')
    expect(drawn('refused').classes).toContain('text-danger')
    expect(drawn('pending').classes).toContain('text-target')
  })

  it('turns only while running', () => {
    expect(
      mount(VStatusIcon, { props: { status: 'running' } })
        .find('svg')
        .classes(),
    ).toContain('animate-spin')
    expect(
      mount(VStatusIcon, { props: { status: 'pending' } })
        .find('svg')
        .classes(),
    ).not.toContain('animate-spin')
  })
})
