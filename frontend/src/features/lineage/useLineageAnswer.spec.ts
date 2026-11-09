import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { buildingRetryMs, useLineageAnswer } from './useLineageAnswer'

type Reply = { state: 'ready' | 'building'; value?: string }

const ok = (data: Reply) => ({ data, response: new Response(null, { status: 200 }) })

function harness(
  send: (query: string, signal: AbortSignal) => Promise<ReturnType<typeof ok>>,
  start: string | null = 'a',
  keeps?: (before: string, after: string) => boolean,
) {
  const question = ref<string | null>(start)
  let state: ReturnType<typeof useLineageAnswer<string, Reply>> | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useLineageAnswer(() => question.value, send, keeps)
        return () => h('div')
      },
    }),
  )
  if (state === undefined) throw new Error('not set up')
  return { question, state, wrapper }
}

describe('useLineageAnswer', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('asks again while building and keeps only the built answer', async () => {
    const send = vi
      .fn()
      .mockResolvedValueOnce(ok({ state: 'building' }))
      .mockResolvedValueOnce(ok({ state: 'building' }))
      .mockResolvedValueOnce(ok({ state: 'ready', value: 'graph' }))
    const { state } = harness(send)
    await vi.advanceTimersByTimeAsync(0)
    expect(state.building.value).toBe(true)
    expect(state.answer.value).toBeNull()
    await vi.advanceTimersByTimeAsync(buildingRetryMs)
    expect(state.answer.value).toBeNull()
    await vi.advanceTimersByTimeAsync(buildingRetryMs)
    expect(state.building.value).toBe(false)
    expect(state.answer.value).toEqual({ state: 'ready', value: 'graph' })
    expect(state.asked.value).toBe(3)
    await vi.advanceTimersByTimeAsync(buildingRetryMs * 3)
    expect(send).toHaveBeenCalledTimes(3)
  })

  it('says why when refused', async () => {
    const send = vi.fn().mockResolvedValue({
      error: { code: 'access.database_not_granted', message: 'Not granted.' },
      response: new Response(null, { status: 403 }),
    })
    const { state } = harness(send)
    await vi.advanceTimersByTimeAsync(0)
    expect(state.failure.value).toBe('Not granted.')
    expect(state.refusal.value).toBe('access.database_not_granted')
  })

  it('says so when the service cannot be reached', async () => {
    const { state } = harness(vi.fn().mockRejectedValue(new Error('offline')))
    await vi.advanceTimersByTimeAsync(0)
    expect(state.failure.value).toContain('Could not reach')
  })

  it('drops the old question in flight, and its answer unless it keeps', async () => {
    const signals: AbortSignal[] = []
    const send = vi.fn((query: string, signal: AbortSignal) => {
      signals.push(signal)
      return query === 'a'
        ? Promise.resolve(ok({ state: 'ready', value: 'a' }))
        : new Promise<ReturnType<typeof ok>>(() => undefined)
    })
    const kept = harness(send, 'a', () => true)
    await vi.advanceTimersByTimeAsync(0)
    kept.question.value = 'b'
    await nextTick()
    expect(kept.state.answer.value?.value).toBe('a')

    const dropped = harness(send, 'a')
    await vi.advanceTimersByTimeAsync(0)
    dropped.question.value = 'b'
    await nextTick()
    expect(dropped.state.answer.value).toBeNull()
    dropped.question.value = 'c'
    await nextTick()
    expect(signals.at(-2)?.aborted).toBe(true)
  })

  it('stops asking when the page goes away', async () => {
    const send = vi.fn().mockResolvedValue(ok({ state: 'building' }))
    const { wrapper } = harness(send)
    await vi.advanceTimersByTimeAsync(0)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(buildingRetryMs * 4)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('asks nothing without a question', async () => {
    const send = vi.fn()
    harness(send, null)
    await vi.advanceTimersByTimeAsync(0)
    expect(send).not.toHaveBeenCalled()
  })
})
