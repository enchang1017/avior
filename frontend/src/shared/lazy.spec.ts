import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { h } from 'vue'

import DelayedLoader from './DelayedLoader.vue'
import { lazy } from './lazy'
import { loaderDelayMs } from './loaderDelay'

const Loaded = () => h('p', { class: 'loaded' }, 'arrived')

function pending() {
  let arrive: () => void = () => undefined
  const chunk = new Promise<typeof Loaded>((resolve) => (arrive = () => resolve(Loaded)))
  return { loader: () => chunk, arrive }
}

function host(component: ReturnType<typeof lazy>) {
  return mount(() => h('div', [h(component)]))
}

describe('lazy', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  // FEAT-074 AC-4: a chunk still loading says so, once the wait is worth mentioning.
  it('shows the loader with its label only after the delay, then the component', async () => {
    const { loader, arrive } = pending()
    const wrapper = host(lazy(loader, 'Loading the rows'))

    expect(wrapper.find('[role="status"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(loaderDelayMs)
    await vi.dynamicImportSettled()
    await flushPromises()
    expect(wrapper.find('[role="status"]').attributes('aria-label')).toBe('Loading the rows')

    arrive()
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('.loaded').exists()).toBe(true)
  })

  it('never shows the loader for a chunk that arrives within the delay', async () => {
    const { loader, arrive } = pending()
    const wrapper = host(lazy(loader, 'Loading the rows', 'inline'))

    arrive()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(loaderDelayMs)

    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.find('.loaded').exists()).toBe(true)
  })

  it('draws a delayed loader only once the wait has outlasted the delay', async () => {
    const wrapper = mount(DelayedLoader, {
      props: { size: 'section', label: 'Reading the lineage' },
    })

    await vi.advanceTimersByTimeAsync(loaderDelayMs - 1)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(1)
    await vi.dynamicImportSettled()
    await flushPromises()
    expect(wrapper.find('[role="status"]').attributes('aria-label')).toBe('Reading the lineage')
  })
})
