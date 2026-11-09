import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VTrajectory from './VTrajectory.vue'
import { writeTrajectory } from './trajectory'

describe('VTrajectory', () => {
  it('draws a row: done stars solid, the current one amber, what is left dashed', () => {
    const row = mount(VTrajectory, { props: { trajectory: writeTrajectory('pending') } })
    expect(row.findAll('[data-state="done"]')).toHaveLength(2)
    expect(row.find('[data-state="current"]').classes()).toContain('fill-target')
    expect(row.findAll('line[stroke-dasharray="2 2"]')).toHaveLength(1)
    expect(row.find('.caption').text()).toBe('Awaiting approval')
    expect(row.find('svg').attributes('aria-label')).toContain('Approval waiting')
  })

  it('crosses the step where a request stopped', () => {
    const row = mount(VTrajectory, { props: { trajectory: writeTrajectory('rejected') } })
    expect(row.find('g[data-state="stopped"]').findAll('line')).toHaveLength(2)
    expect(row.find('.caption').classes()).toContain('text-danger')
  })

  it('lists the approval steps with a line under each, and pulses only the current one', () => {
    const steps = mount(VTrajectory, {
      props: {
        trajectory: writeTrajectory('pending'),
        layout: 'steps',
        details: ['chen.yu · 09-20 08:30', '1 row'],
      },
    })
    expect(steps.findAll('li')).toHaveLength(4)
    expect(steps.text()).toContain('chen.yu · 09-20 08:30')
    expect(steps.findAll('.animate-pulse')).toHaveLength(1)
    expect(steps.find('li[data-state="current"] .animate-pulse').exists()).toBe(true)
  })

  it('puts a pulse under the current step, for a row to wake on hover', () => {
    const pending = mount(VTrajectory, { props: { trajectory: writeTrajectory('pending') } })
    expect(pending.find('.row-pulse').attributes('style')).toContain('translateX(56px)')
    const applied = mount(VTrajectory, { props: { trajectory: writeTrajectory('applied') } })
    expect(applied.find('.row-pulse').exists()).toBe(false)
  })
})
