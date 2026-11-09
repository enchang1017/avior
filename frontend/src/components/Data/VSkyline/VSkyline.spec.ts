import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import VSkyline from './VSkyline.vue'

const buckets = Array.from({ length: 10 }, (_, i) => ({
  start: new Date(Date.UTC(2026, 8, 18, 20 + i)).toISOString(),
  allowed: i,
  denied: i % 3 === 0 ? 1 : 0,
}))

function render(span: { first: number; last: number } | null = null) {
  const sky = mount(VSkyline, { props: { buckets, span }, attachTo: document.body })
  const svg = sky.find('svg').element as SVGSVGElement
  svg.getBoundingClientRect = () => ({ left: 0, width: 1000 }) as DOMRect
  svg.setPointerCapture = () => undefined
  return sky
}

const pointer = (clientX: number) => ({ clientX, button: 0, pointerId: 1 })

describe('VSkyline', () => {
  it('stacks allowed and refused bars and counts both', async () => {
    const sky = render()
    expect(sky.findAll('rect.fill-danger')).toHaveLength(4)
    expect(sky.findAll('rect.fill-fg-muted')).toHaveLength(9)
    await vi.waitFor(() => expect(sky.find('.allowed-total').text()).toBe('45'), { timeout: 2000 })
    expect(sky.find('.denied-total').text()).toBe('4')
    expect(sky.text()).toContain('09-19')
  })

  it('draws the span it is given', () => {
    const brush = render({ first: 2, last: 4 }).find('rect.brush')
    expect(brush.attributes('x')).toBe('200')
    expect(brush.attributes('width')).toBe('300')
  })

  it('brushes by dragging and says so only on release', async () => {
    const sky = render()
    const svg = sky.find('svg')
    await svg.trigger('pointerdown', pointer(650))
    await svg.trigger('pointermove', pointer(420))
    await svg.trigger('pointermove', pointer(250))
    expect(sky.emitted('brush')).toBeUndefined()
    expect(sky.find('rect.brush').attributes('data-first')).toBe('2')
    await svg.trigger('pointerup', pointer(250))
    expect(sky.emitted('brush')).toEqual([[{ first: 2, last: 6 }]])
    expect(sky.find('rect.brush').exists()).toBe(false)
  })

  it('brushes one hour with a click', async () => {
    const sky = render()
    await sky.find('svg').trigger('pointerdown', pointer(150))
    await sky.find('svg').trigger('pointerup', pointer(150))
    expect(sky.emitted('brush')).toEqual([[{ first: 1, last: 1 }]])
  })

  it('drops a drag that was cancelled, and ignores a move with no drag', async () => {
    const sky = render()
    await sky.find('svg').trigger('pointermove', pointer(150))
    await sky.find('svg').trigger('pointerdown', pointer(150))
    await sky.find('svg').trigger('pointercancel')
    await sky.find('svg').trigger('pointerup', pointer(150))
    expect(sky.emitted('brush')).toBeUndefined()
  })

  it('ignores anything but the main button', async () => {
    const sky = render()
    await sky.find('svg').trigger('pointerdown', { clientX: 150, button: 2, pointerId: 1 })
    expect(sky.find('rect.brush').exists()).toBe(false)
  })

  it('clears on escape', async () => {
    const sky = render({ first: 1, last: 1 })
    await sky.find('svg').trigger('keydown', { key: 'Escape' })
    expect(sky.emitted('clear')).toHaveLength(1)
  })
})
