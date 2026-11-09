import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, reactive, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const route = reactive<{ query: Record<string, unknown> }>({ query: {} })
const replace = vi.fn((to: { query: Record<string, unknown> }) => {
  route.query = to.query
  return Promise.resolve()
})
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace }) }))

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { useAuditSky } = await import('./useAuditSky')
type Log = Parameters<typeof useAuditSky>[1]

const hour = 3_600_000
const base = Date.parse('2026-09-18T00:00:00Z')
const histogram = {
  from: new Date(base).toISOString(),
  to: new Date(base + 168 * hour).toISOString(),
  bucketSeconds: 3600,
  buckets: Array.from({ length: 168 }, (_, i) => ({
    start: new Date(base + i * hour).toISOString(),
    allowed: 1,
    denied: 0,
  })),
}

function makeLog(): Log {
  const log: Log = {
    brushed: ref(null),
    operator: ref(''),
    statement: ref(''),
    decision: ref(''),
    busy: ref(false),
    apply: vi.fn(() => {
      log.busy.value = true
      return Promise.resolve().then(() => {
        log.busy.value = false
      })
    }),
  }
  return log
}

function harness(scope: 'mine' | 'all', log = makeLog()) {
  let sky: ReturnType<typeof useAuditSky> | undefined
  mount(
    defineComponent({
      setup() {
        sky = useAuditSky(() => scope, log)
        return () => h('div')
      },
    }),
  )
  if (sky === undefined) throw new Error('not set up')
  return { sky, log }
}

describe('useAuditSky', () => {
  beforeEach(() => {
    get.mockReset()
    replace.mockClear()
    route.query = {}
    get.mockResolvedValue({ data: histogram, response: { status: 200 } })
  })

  it('reads the caller’s own histogram on the personal log', async () => {
    const { sky } = harness('mine')
    await vi.waitFor(() => expect(sky.histogram.value).not.toBeNull())
    expect(get).toHaveBeenCalledWith('/audit/mine/histogram')
    expect(sky.span.value).toBeNull()
  })

  it('narrows everyone’s histogram with the table’s own filters', async () => {
    const log = makeLog()
    log.operator.value = ' chen.yu '
    log.decision.value = 'deny'
    harness('all', log)
    await vi.waitFor(() => expect(get).toHaveBeenCalled())
    expect(get).toHaveBeenCalledWith('/audit/histogram', {
      params: { query: { operator: 'chen.yu', decision: 'deny' } },
    })

    log.statement.value = 'member'
    log.busy.value = true
    await nextTick()
    log.busy.value = false
    await nextTick()
    await vi.waitFor(() => expect(get).toHaveBeenCalledTimes(2))
  })

  it('puts a brushed window in the URL and narrows the log to it, once', async () => {
    const { sky, log } = harness('mine')
    await vi.waitFor(() => expect(sky.histogram.value).not.toBeNull())
    await sky.brush({ first: 26, last: 28 })
    await nextTick()
    expect(route.query).toEqual({
      from: '2026-09-19T02:00:00.000Z',
      to: '2026-09-19T05:00:00.000Z',
    })
    expect(log.brushed.value).toEqual({
      from: '2026-09-19T02:00:00.000Z',
      to: '2026-09-19T04:59:59.999999Z',
    })
    expect(log.apply).toHaveBeenCalledTimes(1)
    expect(sky.span.value).toEqual({ first: 26, last: 28 })
    expect(sky.label.value).toBe('09-19 02:00 → 09-19 05:00')
    await nextTick()
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('restores the window from the URL, and clearing it restores the latest page', async () => {
    route.query = { from: '2026-09-19T02:00:00Z', to: '2026-09-19T05:00:00Z', keep: 'x' }
    const { sky, log } = harness('mine')
    expect(log.brushed.value).toEqual({
      from: '2026-09-19T02:00:00.000Z',
      to: '2026-09-19T04:59:59.999999Z',
    })
    await vi.waitFor(() => expect(sky.span.value).toEqual({ first: 26, last: 28 }))

    await sky.clear()
    await nextTick()
    expect(route.query).toEqual({ keep: 'x' })
    expect(log.brushed.value).toBeNull()
    expect(log.apply).toHaveBeenCalledTimes(2)
  })

  it('says why the sky is missing', async () => {
    get.mockResolvedValueOnce({
      data: undefined,
      error: { code: 'forbidden', message: 'No.' },
      response: { status: 403 },
    })
    const refused = harness('all')
    await vi.waitFor(() => expect(refused.sky.failure.value).toBe('No.'))

    get.mockRejectedValueOnce(new Error('offline'))
    const unreachable = harness('mine')
    await vi.waitFor(() => expect(unreachable.sky.failure.value).toContain('Could not reach'))
  })
})
