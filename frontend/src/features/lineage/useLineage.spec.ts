import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

import type { LineageGraph } from './types'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { useLineageGraph, useLineagePath } = await import('./useLineage')
const { useLineageRoutine } = await import('./useLineageRoutine')
type GraphQuestion = Parameters<typeof useLineageGraph>[0] extends () => infer Q ? Q : never

const ok = (data: unknown) => ({ data, response: new Response(null, { status: 200 }) })

const mounted: { unmount: () => void }[] = []

function host<T>(setup: () => T): T {
  let state: T | undefined
  const wrapper = mount(
    defineComponent({
      setup() {
        state = setup()
        return () => h('div')
      },
    }),
  )
  mounted.push(wrapper)
  if (state === undefined) throw new Error('not set up')
  return state
}

const base: NonNullable<GraphQuestion> = {
  instanceId: 'i',
  database: 'db',
  focus: { schema: 'public', name: 'Bet Ledger', kind: 'table' },
  hops: 1,
  kinds: null,
  confidence: null,
  extensions: false,
}

const answer: LineageGraph = {
  state: 'ready',
  focus: 'table:bet',
  nodes: [
    { id: 'table:bet', kind: 'table', schema: 'public', name: 'bet', hop: 0 },
    { id: 'table:hub', kind: 'table', schema: 'public', name: 'hub', hop: 1, hidden: 2 },
  ],
  edges: [
    {
      from: 'table:hub',
      to: 'table:bet',
      kind: 'foreign_key',
      access: 'reference',
      confidence: 'exact',
    },
  ],
}

const queryOf = (call: number) =>
  (get.mock.calls[call]?.[1] as { params: { query: Record<string, unknown> } }).params.query

const settle = () => vi.advanceTimersByTimeAsync(0)

beforeEach(() => {
  get.mockReset()
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
})

describe('useLineageGraph', () => {
  it('asks only for what differs from the defaults, and clusters three hops', async () => {
    get.mockResolvedValue(ok(answer))
    const question = ref<NonNullable<GraphQuestion>>(base)
    host(() => useLineageGraph(() => question.value))
    await settle()
    expect(queryOf(0)).toEqual({
      instanceId: 'i',
      database: 'db',
      schema: 'public',
      name: 'Bet Ledger',
      kind: 'table',
    })

    question.value = { ...base, hops: 3, kinds: ['table'], confidence: ['exact'], extensions: true }
    await settle()
    expect(queryOf(1)).toEqual({
      instanceId: 'i',
      database: 'db',
      schema: 'public',
      name: 'Bet Ledger',
      kind: 'table',
      hops: 3,
      cluster: true,
      kinds: ['table'],
      confidence: ['exact'],
      extensions: true,
    })
  })

  it('draws a hub neighbours into the answer it already has', async () => {
    get.mockResolvedValueOnce(ok(answer)).mockResolvedValueOnce(
      ok({
        state: 'ready',
        nodes: [{ id: 'table:x', kind: 'table', schema: 'public', name: 'x', hop: 1 }],
        edges: [
          {
            from: 'table:x',
            to: 'table:hub',
            kind: 'foreign_key',
            access: 'reference',
            confidence: 'exact',
          },
        ],
      }),
    )
    const state = host(() => useLineageGraph(() => base))
    await settle()
    await state.expand('table:hub')
    expect(queryOf(1)['expand']).toBe('table:hub')
    expect(state.graph.value?.nodes?.map((node) => node.id)).toEqual([
      'table:bet',
      'table:hub',
      'table:x',
    ])
    expect(state.expanding.value).toBeNull()
  })

  it('says why an expansion failed and keeps what it drew', async () => {
    get.mockResolvedValueOnce(ok(answer)).mockResolvedValueOnce({
      error: { code: 'lineage.object_unknown', message: 'No hub.' },
      response: new Response(null, { status: 404 }),
    })
    const state = host(() => useLineageGraph(() => base))
    await settle()
    await state.expand('table:hub')
    expect(state.failure.value).toBe('No hub.')
    expect(state.graph.value).toEqual(answer)
    get.mockRejectedValueOnce(new Error('offline'))
    await state.expand('table:hub')
    expect(state.failure.value).toContain('Could not reach')
  })

  it('keeps the answer while the focus moves in one database, and drops it for another', async () => {
    get.mockResolvedValueOnce(ok(answer)).mockImplementation(() => new Promise(() => undefined))
    const question = ref<NonNullable<GraphQuestion>>(base)
    const state = host(() => useLineageGraph(() => question.value))
    await settle()
    question.value = { ...base, focus: { schema: 'public', name: 'hub', kind: 'table' } }
    await nextTick()
    expect(state.graph.value).toEqual(answer)
    question.value = { ...base, database: 'other' }
    await nextTick()
    expect(state.graph.value).toBeNull()
  })

  it('expands nothing before there is an answer', async () => {
    get.mockImplementation(() => new Promise(() => undefined))
    const state = host(() => useLineageGraph(() => base))
    await state.expand('table:hub')
    expect(get.mock.calls.length).toBe(1)
  })
})

describe('the path and routine questions', () => {
  it('asks for them as posed', async () => {
    get.mockResolvedValue(ok({ state: 'ready' }))
    host(() =>
      useLineagePath(() => ({
        instanceId: 'i',
        database: 'db',
        from: { schema: 'public', name: 'bet', kind: 'table' },
        to: { schema: 'public', name: 'grant', kind: 'routine', signature: '' },
        maxHops: 4,
      })),
    )
    host(() => useLineageRoutine(() => ({ instanceId: 'i', database: 'db', routine: 'r' })))
    await settle()
    expect(get.mock.calls.map((call) => call[0])).toEqual(['/lineage/path', '/lineage/routine'])
    expect(queryOf(0)).toEqual({
      instanceId: 'i',
      database: 'db',
      fromSchema: 'public',
      fromName: 'bet',
      fromKind: 'table',
      toSchema: 'public',
      toName: 'grant',
      toKind: 'routine',
      toSignature: '',
      maxHops: 4,
    })
  })
})
