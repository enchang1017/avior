import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import VKpiCard from './VKpiCard.vue'

function render(props: Partial<InstanceType<typeof VKpiCard>['$props']> = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:any(.*)*', component: { render: () => null } }],
  })
  return mount(VKpiCard, {
    props: { label: 'p95 elapsed', value: '240 ms', ...props },
    global: { plugins: [router] },
  })
}

describe('VKpiCard', () => {
  it('shows its label, value and hint', () => {
    const card = render({ hint: 'over executed statements' })
    expect(card.text()).toContain('p95 elapsed')
    expect(card.find('[data-testid="kpi-value"]').text()).toBe('240 ms')
    expect(card.text()).toContain('over executed statements')
    expect(card.element.tagName).toBe('DIV')
  })

  it('colours the delta by whether the move is good news', () => {
    const good = render({ delta: { text: '12%', direction: 'down', good: true } })
    const chip = good.find('[data-testid="kpi-delta"]')
    expect(chip.classes()).toContain('text-ok')
    expect(chip.text()).toContain('↓')
    expect(chip.text()).toContain('down')
    expect(chip.text()).toContain('12%')
    const bad = render({ delta: { text: '3%', direction: 'up', good: false } })
    expect(bad.find('[data-testid="kpi-delta"]').classes()).toContain('text-danger')
    const neutral = render({ delta: { text: '0', direction: 'flat', good: null } })
    expect(neutral.find('[data-testid="kpi-delta"]').classes()).toContain('text-fg-muted')
    expect(render().find('[data-testid="kpi-delta"]').exists()).toBe(false)
  })

  it('draws a sparkline only when there are two points or more', () => {
    expect(
      render({ spark: [1, 2, 3] })
        .find('svg.sparkline')
        .exists(),
    ).toBe(true)
    expect(
      render({ spark: [1] })
        .find('svg.sparkline')
        .exists(),
    ).toBe(false)
    expect(render({ spark: null }).find('svg.sparkline').exists()).toBe(false)
  })

  it('is one link when it leads somewhere', () => {
    const card = render({ to: '/observatory/last-used?flagged=1' })
    expect(card.element.tagName).toBe('A')
    expect(card.attributes('href')).toBe('/observatory/last-used?flagged=1')
  })

  it('holds its shape while loading', () => {
    const card = render({ loading: true })
    expect(card.find('[data-testid="kpi-skeleton"]').attributes('role')).toBe('status')
    expect(card.find('[data-testid="kpi-value"]').exists()).toBe(false)
    expect(card.attributes('aria-busy')).toBe('true')
  })
})
