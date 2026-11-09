import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VResultPane, { type ResultTab } from '../VResultPane.vue'

const slots = {
  readout: '<span class="ro">ALLOWED</span>',
  result: '<p class="r">rows</p>',
  gauge: '<p class="g">gauge</p>',
  plan: '<p class="p">plan</p>',
  messages: '<p class="m">messages</p>',
}

describe('VResultPane', () => {
  it('shows the open tab and the readout, and nothing of the others', () => {
    const pane = mount(VResultPane, { props: { modelValue: 'result' }, slots })
    expect(pane.find('.r').exists()).toBe(true)
    expect(pane.find('.p').exists()).toBe(false)
    expect(pane.find('.ro').text()).toBe('ALLOWED')
  })

  // The Plan tab is where the cost gauge goes, above the plan itself.
  it('puts the gauge above the plan', () => {
    const pane = mount(VResultPane, { props: { modelValue: 'plan' }, slots })
    const html = pane.find('.pane-plan').html()
    expect(html.indexOf('gauge')).toBeLessThan(html.indexOf('plan</p>'))
  })

  it('counts messages on their tab and switches tabs', async () => {
    const pane = mount(VResultPane, {
      props: {
        modelValue: 'result' as ResultTab,
        messages: 2,
        'onUpdate:modelValue': (next: string) => pane.setProps({ modelValue: next as ResultTab }),
      },
      slots,
    })
    expect(pane.findAll('[role="tab"]').map((tab) => tab.text())).toEqual([
      'Result',
      'Plan',
      'Messages 2',
    ])
    await pane.findAll('[role="tab"]')[2]?.trigger('click')
    expect(pane.find('.m').exists()).toBe(true)
  })
})
