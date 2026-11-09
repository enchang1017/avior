import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref, type Ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { countAt, countUpMs, useCountUp } from './countUp'

function harness(start: number) {
  const value = ref(start)
  const held: { shown: Ref<number> | null } = { shown: null }
  const wrapper = mount(
    defineComponent({
      setup() {
        held.shown = useCountUp(() => value.value)
        return () => h('span')
      },
    }),
  )
  return { value, shown: () => held.shown?.value, wrapper }
}

describe('a count that ticks up once', () => {
  const original = Object.getOwnPropertyDescriptor(window, 'matchMedia')
  afterEach(() => {
    if (original === undefined) Reflect.deleteProperty(window, 'matchMedia')
    else Object.defineProperty(window, 'matchMedia', original)
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('eases out towards its target and lands on it', () => {
    expect(countAt(100, 0)).toBe(0)
    expect(countAt(100, countUpMs / 2)).toBe(88)
    expect(countAt(100, countUpMs)).toBe(100)
    expect(countAt(100, countUpMs * 2)).toBe(100)
    expect(countAt(100, -5)).toBe(0)
    expect(countAt(7, 10, 0)).toBe(7)
  })

  it('counts up the first time, then follows the value exactly', async () => {
    const { value, shown } = harness(40)
    await vi.waitFor(() => expect(shown()).toBe(40), { timeout: 2000 })
    value.value = 12
    await nextTick()
    expect(shown()).toBe(12)
  })

  it('times the count by its frames, whatever clock performance.now reads', () => {
    const frames: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
      frames.push(callback),
    )
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    vi.spyOn(performance, 'now').mockReturnValue(1_000_000)
    const { shown } = harness(40)
    const tick = (at: number) => frames.shift()?.(at)

    tick(5)
    expect(shown()).toBe(0)
    tick(5 + countUpMs / 2)
    expect(shown()).toBe(35)
    tick(5 + countUpMs)
    expect(shown()).toBe(40)
    expect(frames).toHaveLength(0)
  })

  it('waits for a value, and stops counting when it goes away', async () => {
    const { value, shown, wrapper } = harness(0)
    expect(shown()).toBe(0)
    value.value = 500
    await nextTick()
    wrapper.unmount()
  })

  it('never counts under reduced motion', async () => {
    Object.defineProperty(window, 'matchMedia', {
      value: () => ({ matches: true }),
      configurable: true,
    })
    const { shown } = harness(40)
    await nextTick()
    expect(shown()).toBe(40)
  })
})
