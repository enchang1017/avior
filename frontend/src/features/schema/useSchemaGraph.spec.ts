import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { isIsolated, useSchemaGraph } = await import('./useSchemaGraph')
const { collapsedKey, useChartCollapsed } = await import('./chartCollapsed')

const graph = { nodes: [], edges: [], total: { nodes: 0, edges: 0 }, truncated: false }

function harness(database: string | null, open = true) {
  const db = ref(database)
  const isOpen = ref(open)
  let state: ReturnType<typeof useSchemaGraph> | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        state = useSchemaGraph(
          () => 'instance-1',
          () => db.value,
          () => isOpen.value,
        )
        return () => h('div')
      },
    }),
  )
  if (state === undefined) throw new Error('not set up')
  return { db, isOpen, state, wrapper }
}

describe('useSchemaGraph', () => {
  beforeEach(() => {
    get.mockReset()
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('asks for the graph after the current task, so the tree asks first', async () => {
    get.mockResolvedValue({ data: graph, response: { status: 200 } })
    const { state } = harness('aviorfixture')
    expect(get).not.toHaveBeenCalled()
    await vi.runAllTimersAsync()
    expect(get).toHaveBeenCalledWith('/schema/graph', {
      params: { query: { instanceId: 'instance-1', database: 'aviorfixture' } },
      signal: expect.any(AbortSignal) as AbortSignal,
    })
    expect(state.graph.value).toEqual(graph)
  })

  it('asks for nothing while collapsed or with no database', async () => {
    harness('aviorfixture', false)
    harness(null, true)
    await vi.runAllTimersAsync()
    expect(get).not.toHaveBeenCalled()
  })

  it('drops the graph in flight when the database changes', async () => {
    const signals: AbortSignal[] = []
    get.mockImplementation((_: string, options: { signal: AbortSignal }) => {
      signals.push(options.signal)
      return new Promise(() => undefined)
    })
    const { db } = harness('aviorfixture')
    await vi.runAllTimersAsync()
    db.value = 'aviorspare'
    await nextTick()
    await vi.runAllTimersAsync()
    expect(signals).toHaveLength(2)
    expect(signals[0]?.aborted).toBe(true)
  })

  it('says why when it is refused or unreachable', async () => {
    get.mockResolvedValueOnce({
      data: undefined,
      error: { code: 'x', message: 'Not yours.' },
      response: { status: 403 },
    })
    const refused = harness('aviorspare')
    await vi.runAllTimersAsync()
    expect(refused.state.failure.value).toBe('Not yours.')

    get.mockRejectedValueOnce(new Error('offline'))
    const unreachable = harness('aviorfixture')
    await vi.runAllTimersAsync()
    expect(unreachable.state.failure.value).toContain('Could not reach')
  })

  it('stops when the host goes away', async () => {
    get.mockResolvedValue({ data: graph, response: { status: 200 } })
    const { wrapper } = harness('aviorfixture')
    wrapper.unmount()
    await vi.runAllTimersAsync()
    expect(get).not.toHaveBeenCalled()
  })
})

describe('useChartCollapsed', () => {
  it('remembers the choice in this browser', async () => {
    window.localStorage.removeItem(collapsedKey)
    const collapsed = useChartCollapsed()
    expect(collapsed.value).toBe(false)
    collapsed.value = true
    await nextTick()
    expect(window.localStorage.getItem(collapsedKey)).toBe('true')
    expect(useChartCollapsed().value).toBe(true)
  })
})

describe('isIsolated', () => {
  const ref = (name: string) => ({ schema: 'public', name })
  const linked = {
    nodes: [],
    edges: [
      { from: ref('bet'), to: ref('member'), kind: 'foreign_key' as const },
      { from: ref('lonely'), to: ref('lonely'), kind: 'foreign_key' as const },
    ],
    total: { nodes: 3, edges: 2 },
    truncated: false,
  }

  it('says a target with no edge but to itself is isolated', () => {
    expect(isIsolated(linked, ref('lonely'))).toBe(true)
    expect(isIsolated(linked, ref('absent'))).toBe(true)
  })

  it('says a target that references or is referenced is not', () => {
    expect(isIsolated(linked, ref('bet'))).toBe(false)
    expect(isIsolated(linked, ref('member'))).toBe(false)
  })

  it('decides nothing without an answer or a target', () => {
    expect(isIsolated(null, ref('bet'))).toBe(false)
    expect(isIsolated(linked, null)).toBe(false)
  })
})
