import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VPopover from '../VPopover.vue'

function render() {
  return mount(VPopover, {
    props: { label: 'Rules' },
    slots: { trigger: 'Guarded', default: '<p class="inside">read-only</p>' },
    attachTo: document.body,
  })
}

describe('VPopover', () => {
  it('opens on its trigger and stays open while it is read', async () => {
    const wrapper = render()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)

    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('[role="dialog"]').attributes('aria-label')).toBe('Rules')

    await wrapper.find('.inside').trigger('mousedown')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('closes on a click elsewhere and on Escape', async () => {
    const wrapper = render()
    await wrapper.find('button.trigger').trigger('click')
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)

    await wrapper.find('button.trigger').trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens above with a wide offset when asked, clear of the bar it sits in', async () => {
    const wrapper = mount(VPopover, {
      props: { label: 'Dynamic', side: 'above', offset: 'wide' },
      slots: { trigger: 'Chip', default: '<p>routines</p>' },
    })
    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('.popover').classes()).toEqual(
      expect.arrayContaining(['bottom-full', 'mb-3']),
    )
  })
})
