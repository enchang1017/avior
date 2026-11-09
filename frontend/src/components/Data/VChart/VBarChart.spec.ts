import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import VBarChart from './VBarChart.vue'
import type { BarDatum } from './types'

const data: BarDatum[] = [
  { id: 'alice', label: 'alice@example.com', value: 40, detail: '12 refused' },
  {
    id: 'orders',
    label: 'orders',
    value: 20,
    segments: [
      { id: 'table', label: 'Tables', tone: 'primary', value: 15 },
      { id: 'view', label: 'Views', tone: 'series', value: 5 },
    ],
  },
]

function render(props: Partial<InstanceType<typeof VBarChart>['$props']> = {}) {
  return mount(VBarChart, {
    props: {
      data,
      caption: 'Top operators',
      formatValue: (value: number) => `${value} st`,
      ...props,
    },
    attachTo: document.body,
  })
}

function reduceMotion(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', { value: () => ({ matches }), configurable: true })
}

const original = Object.getOwnPropertyDescriptor(window, 'matchMedia')

afterEach(() => {
  if (original === undefined) Reflect.deleteProperty(window, 'matchMedia')
  else Object.defineProperty(window, 'matchMedia', original)
  document.body.innerHTML = ''
})

describe('VBarChart', () => {
  it('draws a row per datum in order, scaled to the longest', () => {
    const chart = render()
    const rows = chart.findAll('button.bar-row')
    expect(rows.map((row) => row.attributes('data-id'))).toEqual(['alice', 'orders'])
    const fills = chart.findAll('.bar-fill')
    expect(fills[0]?.attributes('style')).toContain('scaleX(1)')
    expect(fills[1]?.attributes('style')).toContain('scaleX(0.5)')
    expect(rows[0]?.text()).toContain('40 st')
    expect(rows[0]?.text()).toContain('12 refused')
    expect(rows[0]?.find('[title]').attributes('title')).toBe('alice@example.com')
    expect(chart.find('[data-testid="chart-summary"]').text()).toContain('alice@example.com')
  })

  it('stacks segments in their tones', () => {
    const segments = render().findAll('.bar-fill')[1]?.findAll('span') ?? []
    expect(segments.map((segment) => segment.classes())).toEqual([
      expect.arrayContaining(['bg-primary']),
      expect.arrayContaining(['bg-fg-soft']),
    ])
    expect(segments[0]?.attributes('style')).toContain('flex-grow: 15')
  })

  it('breaks a bar down on hover and on focus', async () => {
    const chart = render()
    const rows = chart.findAll('button.bar-row')
    await rows[1]?.trigger('mouseenter')
    const tooltip = chart.find('[data-testid="chart-tooltip"]')
    expect(tooltip.text()).toContain('Tables')
    expect(tooltip.text()).toContain('15 st')
    expect(tooltip.text()).toContain('20 st')
    await rows[1]?.trigger('mouseleave')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(false)
    await rows[0]?.trigger('focus')
    expect(chart.find('[data-testid="chart-tooltip"]').text()).toContain('12 refused')
    await rows[1]?.trigger('blur')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(true)
    await rows[0]?.trigger('blur')
    expect(chart.find('[data-testid="chart-tooltip"]').exists()).toBe(false)
  })

  it('opens a bar on click, which a button also answers to Enter', async () => {
    const chart = render()
    await chart.findAll('button.bar-row')[1]?.trigger('click')
    expect(chart.emitted('select')).toEqual([['orders']])
    expect(chart.find('button.bar-row').attributes('type')).toBe('button')
  })

  it('shows its legend when given one', () => {
    expect(render().find('[data-testid="chart-legend"]').exists()).toBe(false)
    const legend = render({ legend: [{ id: 'table', label: 'Tables', tone: 'primary' }] })
    expect(legend.find('[data-testid="chart-legend"]').text()).toBe('Tables')
  })

  it('shows a still placeholder while loading and a sentence when empty', () => {
    const loading = render({ loading: true }).find('[data-testid="chart-skeleton"]')
    expect(loading.attributes('role')).toBe('status')
    expect(loading.html()).not.toContain('shimmer')
    expect(render({ data: [] }).find('[data-testid="chart-empty"]').text()).toBe(
      'No data in this range.',
    )
  })

  it('grows the bars in once, and not under reduced motion', async () => {
    reduceMotion(false)
    const chart = render()
    const fill = chart.find('.bar-fill')
    expect(fill.classes()).toContain('draw-in')
    await fill.trigger('animationend')
    expect(fill.classes()).not.toContain('draw-in')
    reduceMotion(true)
    expect(render().find('.bar-fill').classes()).not.toContain('draw-in')
  })
})
