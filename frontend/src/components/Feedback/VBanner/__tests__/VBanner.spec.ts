import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VBanner from '../VBanner.vue'

describe('VBanner', () => {
  // Three views had rendered every kind of message in the same red. A refusal
  // the operator caused, a disagreement Avior cannot resolve, and a note about
  // what a page does are not the same thing.
  it('draws each tone in its own colours, so the three read differently', () => {
    const drawn = (tone: 'danger' | 'warn' | 'info') =>
      mount(VBanner, { props: { tone } }).classes()

    expect(drawn('danger')).toEqual(expect.arrayContaining(['bg-danger-bg', 'text-danger']))
    expect(drawn('warn')).toEqual(expect.arrayContaining(['bg-warn-bg', 'text-warn']))
    expect(drawn('info')).toEqual(expect.arrayContaining(['bg-surface-sunken', 'text-fg-soft']))
  })

  it('is a refusal unless told otherwise', () => {
    expect(mount(VBanner).classes()).toContain('text-danger')
  })

  // Announced by default, because a banner appears when something the operator
  // did not expect happened. The one that is only a note is not announced, or
  // a screen reader interrupts to read something nobody needs interrupting for.
  it('announces itself unless it is only a note', () => {
    expect(mount(VBanner).attributes('role')).toBe('alert')
    expect(mount(VBanner, { props: { alert: false } }).attributes('role')).toBeUndefined()
  })
})
