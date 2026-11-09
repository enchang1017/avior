import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import { useUtcClock, utcMinute } from './clock'

describe('the UTC clock', () => {
  afterEach(() => vi.useRealTimers())

  it('reads hours and minutes in UTC', () => {
    expect(utcMinute(new Date('2026-09-20T08:30:59Z'))).toBe('08:30')
  })

  // Once a minute, on the minute: never a repaint a second.
  it('moves on the minute and not before', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-20T08:30:40Z'))
    const Probe = defineComponent({
      setup: () => {
        const now = useUtcClock()
        return () => h('span', now.value)
      },
    })
    const wrapper = mount(Probe)
    expect(wrapper.text()).toBe('08:30')

    await vi.advanceTimersByTimeAsync(19_000)
    expect(wrapper.text()).toBe('08:30')
    await vi.advanceTimersByTimeAsync(1_000)
    expect(wrapper.text()).toBe('08:31')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
