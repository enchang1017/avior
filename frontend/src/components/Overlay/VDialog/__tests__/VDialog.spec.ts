import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import VDialog from '../VDialog.vue'

/*
 * jsdom 26 ships HTMLDialogElement with neither showModal nor close: the
 * element exists, the top-layer behaviour does not.
 *
 * The two are stood up here rather than the component carrying a branch that
 * exists only for the test — a component that feature-detects its way around
 * the platform is untested on the path it actually takes in a browser. What the
 * shim cannot stand up (the focus trap, the inert background) is what the
 * Playwright case covers, on a real one.
 */
beforeAll(() => {
  const dialog = window.HTMLDialogElement.prototype
  if (typeof dialog.showModal === 'function') return
  dialog.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute('open', '')
  }
  dialog.close = function close(this: HTMLDialogElement) {
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  }
})

function render(open = false) {
  return mount(VDialog, {
    props: { open, title: 'What each role opens' },
    slots: { default: '<p class="content">Developer runs statements.</p>' },
    attachTo: document.body,
  })
}

function element(dialog: ReturnType<typeof render>) {
  return dialog.find('dialog').element as HTMLDialogElement
}

describe('VDialog', () => {
  it('stays shut until it is opened', () => {
    expect(element(render()).open).toBe(false)
  })

  it('opens as a modal, so the background is inert', async () => {
    const dialog = render()

    await dialog.setProps({ open: true })

    expect(element(dialog).open).toBe(true)
  })

  it('shows what it was given', async () => {
    const dialog = render(true)
    await dialog.vm.$nextTick()

    expect(dialog.find('.content').text()).toBe('Developer runs statements.')
    expect(dialog.find('.title').text()).toBe('What each role opens')
  })

  // The parent owns the open state, so every way of dismissing has to reach it.
  // A dialog that closed itself would reopen on the next render.
  it('asks to be closed from the button', async () => {
    const dialog = render(true)

    await dialog.find('.close').trigger('click')

    expect(dialog.emitted('close')).toHaveLength(1)
  })

  it('asks to be closed from escape', async () => {
    const dialog = render(true)

    await dialog.find('dialog').trigger('cancel')

    expect(dialog.emitted('close')).toHaveLength(1)
  })

  // The backdrop is the dialog's own pseudo-element, so a click on it lands on
  // the dialog. Told apart by where it fell, or clicking the content would
  // dismiss it.
  it('closes on a click outside and not on one inside', async () => {
    const dialog = render(true)

    await dialog.find('.content').trigger('click')
    expect(dialog.emitted('close')).toBeUndefined()

    await dialog.find('dialog').trigger('click')
    expect(dialog.emitted('close')).toHaveLength(1)
  })

  it('closes again when it is reopened and shut', async () => {
    const dialog = render(true)
    await dialog.vm.$nextTick()

    await dialog.setProps({ open: false })

    expect(element(dialog).open).toBe(false)
  })

  // FEAT-076. A dialog read over and over only fades, and carries a badge, a
  // status and its acts where the roles page puts them.
  it('fades rather than grows when told to, and grows by default', () => {
    expect(render(true).find('dialog').classes()).toContain('open:animate-appear')
    const fading = mount(VDialog, {
      props: { open: true, title: 'role_shop', motion: 'fade' },
    })
    expect(fading.find('dialog').classes()).toContain('open:animate-fade')
  })

  it('draws a badge, a status and a footer only when given them', () => {
    expect(render(true).find('footer').exists()).toBe(false)
    const full = mount(VDialog, {
      props: { open: true, title: 'role_shop' },
      slots: {
        badge: '<span class="kind">group</span>',
        status: '<span class="state">cannot connect</span>',
        footer: '<button class="act">Drop</button>',
      },
    })
    expect(full.find('header .kind').text()).toBe('group')
    expect(full.find('header .state').text()).toBe('cannot connect')
    expect(full.find('footer .act').text()).toBe('Drop')
  })
})
