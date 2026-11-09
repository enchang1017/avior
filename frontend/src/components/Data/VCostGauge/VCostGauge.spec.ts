import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import VCostGauge from './VCostGauge.vue'

const limits = { maxCost: 10_000, maxRows: 100_000 }
const render = (estimatedCost: number | null, stale = false) =>
  mount(VCostGauge, { props: { measurement: { estimatedCost, estimatedRows: 3, limits }, stale } })

describe('VCostGauge', () => {
  it('sweeps the bar towards the cap and prints both numbers', async () => {
    const gauge = render(312)
    expect(gauge.attributes('data-level')).toBe('clear')
    expect(gauge.find('.fill').attributes('style')).toContain('scaleX(0)')
    await vi.waitFor(() =>
      expect(gauge.find('.fill').attributes('style')).toContain('scaleX(0.0312)'),
    )
    expect(gauge.find('.cost').text()).toBe('312 / 10,000')
    expect(gauge.find('.rows').text()).toBe('3 / 100,000')
  })

  it('names the rule once over the cap', () => {
    const gauge = render(12_000)
    expect(gauge.attributes('data-level')).toBe('over')
    expect(gauge.find('.rule').text()).toBe('cost_limit_exceeded')
    expect(gauge.find('.cost').classes()).toContain('text-danger')
    expect(gauge.find('.cost').classes()).toContain('gauge-alert')
  })

  it('reads no plan with no bar', () => {
    const gauge = render(null)
    expect(gauge.find('[role="meter"]').exists()).toBe(false)
    expect(gauge.find('.cost').text()).toContain('no plan')
  })

  it('dims and says stale after an edit', () => {
    const gauge = render(312, true)
    expect(gauge.attributes('data-stale')).toBe('true')
    expect(gauge.classes()).toContain('opacity-50')
    expect(gauge.text()).toContain('stale')
  })
})
