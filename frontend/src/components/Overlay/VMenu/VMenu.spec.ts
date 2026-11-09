import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VMenu from './VMenu.vue'

/*
 * A menu is small enough that the cases are the whole of it: it opens, it
 * closes the three ways it can be closed, and it tells assistive technology
 * which of those it currently is.
 */
function open() {
  return mount(VMenu, {
    props: { label: 'Your account' },
    slots: { trigger: '<span class="avatar">E</span>', default: '<a href="/x">Tokens</a>' },
    attachTo: document.body,
  })
}

describe('VMenu', () => {
  it('opens and closes on the trigger', async () => {
    const wrapper = open()
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)

    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('[role="menu"]').exists()).toBe(true)

    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
  })

  it('says whether it is open', async () => {
    const wrapper = open()
    const trigger = wrapper.find('button.trigger')

    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(trigger.attributes('aria-haspopup')).toBe('menu')
    expect(trigger.attributes('aria-label')).toBe('Your account')

    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  /*
   * Choosing an item closes it, and the item knows nothing about that.
   *
   * The alternative is every caller remembering to close the menu in each
   * handler, which is a thing to forget once per item added.
   */
  it('closes when something inside it is clicked', async () => {
    const wrapper = open()
    await wrapper.find('button.trigger').trigger('click')

    await wrapper.find('[role="menu"] a').trigger('click')
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
  })

  it('closes on a click outside and on escape', async () => {
    for (const shut of [
      () => document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })),
      () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })),
    ]) {
      const wrapper = open()
      await wrapper.find('button.trigger').trigger('click')
      expect(wrapper.find('[role="menu"]').exists()).toBe(true)

      shut()
      await wrapper.vm.$nextTick()
      expect(wrapper.find('[role="menu"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('hands focus back to its trigger when escape closes it', async () => {
    const wrapper = open()
    await wrapper.find('button.trigger').trigger('click')
    const item = wrapper.find<HTMLAnchorElement>('[role="menu"] a').element
    item.focus()
    expect(document.activeElement).toBe(item)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()

    expect(document.activeElement).toBe(wrapper.find('button.trigger').element)
    wrapper.unmount()
  })

  // A click inside must not close it before the item it landed on has run.
  it('stays open for a click on itself', async () => {
    const wrapper = open()
    await wrapper.find('button.trigger').trigger('click')

    wrapper
      .find('[role="menu"]')
      .element.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[role="menu"]').exists()).toBe(true)
  })

  // A menu near the bottom of the screen has no room below it.
  it('opens above its trigger when asked', async () => {
    const wrapper = mount(VMenu, {
      props: { side: 'above', align: 'left' },
      attachTo: document.body,
    })
    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('[role="menu"]').classes()).toEqual(
      expect.arrayContaining(['bottom-full', 'left-0']),
    )
    wrapper.unmount()
  })
})
