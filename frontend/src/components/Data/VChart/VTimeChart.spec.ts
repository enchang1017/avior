import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import VTimeChart from './VTimeChart.vue'
import type { Series } from './types'

const hour = 3600_000

const series: Series[] = [
  {
    id: 'allowed',
    label: 'Allowed',
    tone: 'primary',
    points: [10, 20, 30, 40].map((y, i) => ({ x: i * hour, y })),
  },
  {
    id: 'refused',
    label: 'Refused',
    tone: 'danger',
    points: [1, null, 3, 4].map((y, i) => ({ x: i * hour, y })),
  },
]

function render(props: Partial<InstanceType<typeof VTimeChart>['$props']> = {}) {
  const chart = mount(VTimeChart, {
    props: {
      series,
      mode: 'line',
      caption: 'Statement volume',
      formatY: (value: number) => `${value} st`,
      formatX: (x: number) => `h${x / hour}`,
      ...props,
    },
    attachTo: document.body,
  })
  const plot = chart.find('[data-testid="chart-plot"]')
  if (plot.exists()) {
    ;(plot.element as HTMLElement).getBoundingClientRect = () =>
      ({ left: 0, width: 600 }) as DOMRect
  }
  return chart
}

function reduceMotion(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({ matches }),
    configurable: true,
  })
}

const original = Object.getOwnPropertyDescriptor(window, 'matchMedia')

afterEach(() => {
  if (original === undefined) Reflect.deleteProperty(window, 'matchMedia')
  else Object.defineProperty(window, 'matchMedia', original)
  document.body.innerHTML = ''
})

describe('VTimeChart', () => {
  it('captions the figure and says the data in words', () => {
    const chart = render()
    expect(chart.find('figcaption').text()).toContain('Statement volume')
    expect(chart.find('[data-testid="chart-summary"]').text()).toContain(
      'Allowed: low 10 st, high 40 st, latest 40 st.',
    )
    expect(chart.find('[data-testid="chart-legend"]').text()).toContain('Refused')
  })

  it('draws a line per series and labels both axes with the formatters', () => {
    const chart = render()
    expect(chart.findAll('path[data-series]')).toHaveLength(2)
    expect(chart.find('path[data-series="refused"]').attributes('d')).toContain('M')
    const labels = chart.findAll('text').map((text) => text.text())
    expect(labels).toContain('0 st')
    expect(labels).toContain('h0')
    expect(labels).toContain('h3')
  })

  it('stacks areas in stacked mode', () => {
    const chart = render({ mode: 'stacked-area' })
    expect(chart.findAll('path.fill-danger\\/30')).toHaveLength(1)
    expect(chart.find('figure').attributes('data-mode')).toBe('stacked-area')
  })

  it('shows every series at the hovered bucket, and opens it on click', async () => {
    const chart = render()
    const plot = chart.find('[data-testid="chart-plot"]')
    await plot.trigger('pointerenter')
    await plot.trigger('pointermove', { clientX: 250 })
    const tooltip = chart.find('[data-testid="chart-tooltip"]')
    expect(tooltip.text()).toContain('h1')
    expect(tooltip.text()).toContain('20 st')
    expect(tooltip.text()).toContain('no value')
    expect(tooltip.attributes('style')).toContain('translate(')
    expect(chart.find('[data-testid="chart-crosshair"]').exists()).toBe(true)
    await plot.trigger('click')
    expect(chart.emitted('select')).toEqual([[hour]])
    await plot.trigger('pointerleave')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(false)
  })

  it('moves by keyboard, speaks the bucket, and opens it with Enter', async () => {
    const chart = render()
    const plot = chart.find('[data-testid="chart-plot"]')
    expect(plot.attributes('tabindex')).toBe('0')
    await plot.trigger('focus')
    expect(chart.find('[data-testid="chart-tooltip"]').text()).toContain('h3')
    await plot.trigger('keydown', { key: 'ArrowLeft' })
    expect(chart.find('[data-testid="chart-tooltip"]').text()).toContain('h2')
    expect(chart.find('[data-testid="chart-spoken"]').text()).toBe(
      'h2: Allowed 30 st, Refused 3 st',
    )
    await plot.trigger('keydown', { key: 'Home' })
    await plot.trigger('keydown', { key: 'ArrowLeft' })
    expect(chart.find('[data-testid="chart-tooltip"]').text()).toContain('h0')
    await plot.trigger('keydown', { key: 'End' })
    await plot.trigger('keydown', { key: 'ArrowRight' })
    await plot.trigger('keydown', { key: 'Tab' })
    await plot.trigger('keydown', { key: 'Enter' })
    expect(chart.emitted('select')).toEqual([[3 * hour]])
    await plot.trigger('pointerleave')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(true)
    await plot.trigger('blur')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(false)
  })

  it('shows a still placeholder while loading', () => {
    const chart = render({ loading: true, height: 120 })
    const skeleton = chart.find('[data-testid="chart-skeleton"]')
    expect(skeleton.attributes('role')).toBe('status')
    expect(skeleton.attributes('style')).toContain('height: 120px')
    expect(skeleton.classes().some((name) => name.startsWith('shimmer'))).toBe(false)
    expect(chart.find('svg').exists()).toBe(false)
  })

  it('says so when there is nothing to draw', () => {
    const zeros: Series[] = [{ ...series[0], points: [{ x: 0, y: 0 }] } as Series]
    expect(render({ series: zeros }).find('[data-testid="chart-empty"]').text()).toBe(
      'No data in this range.',
    )
    expect(
      render({ series: [], emptyText: 'Nothing yet.' }).find('[data-testid="chart-empty"]').text(),
    ).toBe('Nothing yet.')
  })

  it('draws in once, and not again after it ends', async () => {
    reduceMotion(false)
    const chart = render()
    const marks = chart.find('[data-testid="chart-marks"]')
    expect(marks.classes()).toContain('draw-in')
    await marks.trigger('animationend')
    expect(marks.classes()).not.toContain('draw-in')
    await chart.setProps({ loading: true })
    await chart.setProps({ loading: false })
    expect(chart.find('[data-testid="chart-marks"]').classes()).not.toContain('draw-in')
  })

  it('does not draw in under reduced motion', () => {
    reduceMotion(true)
    expect(render().find('[data-testid="chart-marks"]').classes()).not.toContain('draw-in')
  })
})
