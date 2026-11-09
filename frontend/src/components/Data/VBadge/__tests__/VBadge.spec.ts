import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VBadge from '../VBadge.vue'

describe('VBadge', () => {
  it('says what it is given', () => {
    expect(mount(VBadge, { slots: { default: 'pending' } }).text()).toBe('pending')
  })

  // The tone is the only thing a view says, so each has to reach the page as
  // its own colour rather than as the plain chip.
  it('draws each tone in its own colours', () => {
    const drawn = (tone: 'ok' | 'warn' | 'danger' | 'neutral' | 'primary' | 'agent' | 'plain') =>
      mount(VBadge, { props: { tone } }).classes()

    expect(drawn('ok')).toEqual(expect.arrayContaining(['text-ok', 'bg-ok-bg', 'border-ok-line']))
    expect(drawn('warn')).toContain('text-warn')
    expect(drawn('danger')).toContain('text-danger')
    expect(drawn('neutral')).toContain('bg-surface-sunken')
    expect(drawn('primary')).toContain('text-primary')
    expect(drawn('agent')).toEqual(
      expect.arrayContaining(['border-primary-soft', 'bg-surface-sunken']),
    )
    expect(drawn('plain')).toEqual(expect.arrayContaining(['bg-surface-sunken', 'text-fg-soft']))
  })

  it('is a step smaller where the chrome needs it', () => {
    expect(mount(VBadge, { props: { size: 'sm' } }).classes()).toContain('text-2xs')
    expect(mount(VBadge).classes()).toContain('text-xs')
  })
})
