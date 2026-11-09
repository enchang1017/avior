import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { RouterLinkStub } from '@vue/test-utils'

import VButton from '../VButton.vue'

const stubs = { RouterLink: RouterLinkStub }

describe('VButton', () => {
  it('is a ghost button of ordinary size unless told otherwise', () => {
    const button = mount(VButton, { global: { stubs } })

    expect(button.classes()).toEqual(
      expect.arrayContaining(['border-line-strong', 'bg-transparent', 'h-control']),
    )
    expect(button.classes()).not.toContain('h-control-sm')
  })

  // The sizes had already come apart by hand: an ordering control sat beside a
  // picker a quarter taller than it, because one carried the small size and the
  // other could not.
  it('carries its size, so a row of controls is one height', () => {
    const small = mount(VButton, { props: { size: 'sm' }, global: { stubs } }).classes()
    expect(small).toEqual(expect.arrayContaining(['h-control-sm', 'text-xs']))
    expect(small).not.toContain('h-control')
  })

  it('carries its variant', () => {
    const drawn = (variant: 'primary' | 'ghost' | 'bare') =>
      mount(VButton, { props: { variant }, global: { stubs } }).classes()

    expect(drawn('primary')).toEqual(expect.arrayContaining(['bg-primary', 'text-on-primary']))
    expect(drawn('ghost')).toEqual(expect.arrayContaining(['bg-transparent', 'text-fg-soft']))
    expect(drawn('primary')).toContain('shadow-glow-primary')
    expect(drawn('bare')).toEqual(expect.arrayContaining(['bg-transparent', 'text-fg-soft']))
  })

  // The utilities for one property must not meet on one element: which of two
  // wins is decided by Tailwind's sort order, not by the order they are written.
  it('never asks for two colours, grounds or heights at once', () => {
    for (const variant of ['primary', 'ghost', 'bare'] as const) {
      for (const size of ['md', 'sm'] as const) {
        const classes = mount(VButton, { props: { variant, size }, global: { stubs } }).classes()
        const plain = classes.filter((name) => !name.includes(':'))
        expect(plain.filter((name) => /^bg-/u.test(name))).toHaveLength(1)
        expect(plain.filter((name) => /^h-/u.test(name))).toHaveLength(1)
        expect(plain.filter((name) => /^(px|py)-/u.test(name))).toHaveLength(2)
        expect(plain.filter((name) => /^text-(fg|on-primary)/u.test(name))).toHaveLength(1)
      }
    }
  })

  // Red in place of the variant's own text colour, disabled or not: two text
  // colours on one element would leave the winner to Tailwind's sort order.
  it('marks a destructive action so it reads before it is read', () => {
    for (const variant of ['primary', 'ghost', 'bare'] as const) {
      for (const disabled of [false, true]) {
        const classes = mount(VButton, {
          props: { danger: true, variant, disabled },
          global: { stubs },
        }).classes()
        expect(classes).toContain('text-danger')
        expect(classes.filter((name) => /^(disabled:)?text-(fg|on-primary)/u.test(name))).toEqual(
          [],
        )
      }
    }
  })

  it('draws the icon it was given, and none when it was given none', () => {
    const withIcon = mount(VButton, { props: { icon: 'plus' }, global: { stubs } })
    expect(withIcon.find('svg[data-icon="plus"]').exists()).toBe(true)

    expect(mount(VButton, { global: { stubs } }).find('i').exists()).toBe(false)
  })

  /*
   * Going back to a list is navigation, not an action. A link that looks like a
   * button still has to be a link, or middle-click and open-in-new-tab stop
   * working — and somebody who wanted the release in a second tab is left
   * wondering why nothing happened.
   */
  it('is a link when it navigates, and a button when it acts', () => {
    const link = mount(VButton, { props: { to: { name: 'release-pending' } }, global: { stubs } })
    expect(link.findComponent(RouterLinkStub).exists()).toBe(true)
    expect(link.find('button').exists()).toBe(false)

    const action = mount(VButton, { global: { stubs } })
    expect(action.find('button').exists()).toBe(true)
  })

  it('does nothing while disabled', async () => {
    const button = mount(VButton, { props: { disabled: true }, global: { stubs } })

    expect(button.find('button').attributes('disabled')).toBeDefined()
    await button.trigger('click')
    expect(button.emitted('click')).toBeUndefined()
  })

  // A filter bar's button submits its form. Defaulting to submit would make
  // every other button on a form apply it by accident.
  it('is not a submit button unless it is asked to be', () => {
    expect(mount(VButton, { global: { stubs } }).attributes('type')).toBe('button')
    expect(
      mount(VButton, { props: { type: 'submit' }, global: { stubs } }).attributes('type'),
    ).toBe('submit')
  })
})
