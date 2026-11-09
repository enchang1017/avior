import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import type * as sharedApi from '@/shared/api'
import { accessFor, overviewPanels } from './access'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const chart = (name: string) =>
  defineComponent({
    name,
    props: {
      caption: { type: String, default: '' },
      label: { type: String, default: '' },
      loading: { type: Boolean, default: false },
    },
    emits: ['select'],
    setup: (props) => () =>
      h(
        'div',
        { 'data-stub': name, 'data-loading': String(props.loading) },
        props.caption || props.label,
      ),
  })

vi.mock('@/components', async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>()
  return {
    ...actual,
    VKpiCard: chart('VKpiCard'),
    VTimeChart: chart('VTimeChart'),
    VBarChart: chart('VBarChart'),
  }
})

const { default: OverviewTab } = await import('./OverviewTab.vue')

const ok = (data: unknown) => ({ data, response: { status: 200 } })
const totals = { statements: 4, allowed: 3, refused: 1, executed: 3, p50Ms: 1, p95Ms: 2 }
const answers: Record<string, unknown> = {
  '/observatory/usage/series': {
    from: '2026-09-20T11:00:00Z',
    to: '2026-09-27T11:00:00Z',
    bucketSeconds: 3600,
    buckets: [],
    totals,
    previous: totals,
  },
  '/observatory/usage': { from: '', to: '', rows: [] },
  '/observatory/databases': { idleDays: 30, lastRunAt: null, flagsWithheld: null, databases: [] },
  '/observatory/summary': { idleDays: 30, flagsWithheld: null, largestTables: [], idle: [] },
  '/observatory/activity': { generatedAt: '2026-09-27T10:00:00Z', backends: [] },
  '/observatory/statements': { statsSince: null, statements: [] },
  '/observatory/routines': {
    from: '',
    to: '',
    statsSince: null,
    tracking: [],
    routines: [],
    groups: [],
  },
}

async function open(permissions: string[], query: Record<string, string> = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/observatory', name: 'observatory', component: { render: () => null } }],
  })
  await router.push({ name: 'observatory', query })
  const tab = mount(OverviewTab, {
    props: { panels: overviewPanels(accessFor(permissions)) },
    global: { plugins: [router] },
  })
  await flushPromises()
  await vi.dynamicImportSettled()
  await flushPromises()
  return { tab, router }
}

const asked = () => get.mock.calls.map(([path]) => path as string)
const charts = (tab: ReturnType<typeof mount>) =>
  tab.findAll('[data-chart]').map((node) => node.attributes('data-chart'))

beforeEach(() => {
  get.mockReset()
  get.mockImplementation((path: string) => Promise.resolve(ok(answers[path])))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('what the overview draws for whom', () => {
  it('draws every card, every chart and the live panel for an administrator', async () => {
    const { tab } = await open(['audit.read.all'])
    expect(tab.findAll('[data-stub="VKpiCard"]')).toHaveLength(6)
    expect(charts(tab)).toEqual([
      'volume',
      'latency',
      'size',
      'operators',
      'reasons',
      'tables',
      'idle',
      'slowest',
      'routines',
    ])
    expect(tab.find('[data-testid="live-panel"]').exists()).toBe(true)
  })

  // FEAT-071: the owner opened the observatory to every signed-in operator on 2026-09-27.
  it.each([
    ['a developer', ['audit.read.own', 'query.inspect']],
    ['a DBA', ['audit.read.own', 'instance.read']],
    ['a release approver', ['release.approve']],
  ])('draws %s the whole overview too', async (_who, permissions) => {
    const { tab } = await open(permissions)
    expect(tab.findAll('[data-stub="VKpiCard"]')).toHaveLength(6)
    expect(charts(tab)).toEqual([
      'volume',
      'latency',
      'size',
      'operators',
      'reasons',
      'tables',
      'idle',
      'slowest',
      'routines',
    ])
    expect(tab.find('[data-testid="live-panel"]').exists()).toBe(true)
    expect(tab.text()).not.toContain('Usage figures are your own statements.')
  })
})

describe('the toolbar and the refresh', () => {
  it('asks for the window and database in the address', async () => {
    await open(['audit.read.all'], { range: '24h', db: 'aviorfixture' })
    const series = get.mock.calls.find(([path]) => path === '/observatory/usage/series') as [
      string,
      { params: { query: { from: string; to: string; database: string } } },
    ]
    const { from, to, database } = series[1].params.query
    expect(Date.parse(to) - Date.parse(from)).toBe(24 * 3_600_000)
    expect(database).toBe('aviorfixture')
  })

  it('writes a choice into the address', async () => {
    const { tab, router } = await open(['audit.read.all'])
    tab.findComponent({ name: 'VSelect' }).vm.$emit('update:modelValue', '30d')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ range: '30d', db: '', refresh: 'off' })
  })

  it('re-reads only the running statements at the chosen interval', async () => {
    vi.useFakeTimers()
    await open(['audit.read.all'], { refresh: '30' })
    const before = asked().length
    const activity = () => asked().filter((path) => path === '/observatory/activity').length
    const once = activity()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(activity()).toBe(once + 1)
    expect(asked().length).toBe(before + 1)
  })

  it('never re-reads when the refresh is off', async () => {
    vi.useFakeTimers()
    await open(['audit.read.all'])
    const before = asked().length
    await vi.advanceTimersByTimeAsync(120_000)
    expect(asked().length).toBe(before)
  })
})

describe('no card is left loading', () => {
  const roles: Record<string, string[]> = {
    developer: ['audit.read.own'],
    dba: ['audit.read.own', 'instance.read'],
    administrator: ['audit.read.all'],
  }

  it.each(Object.entries(roles))(
    'every card a %s sees resolves once its data has arrived, below the fold unseen',
    async (_role, permissions) => {
      class NeverIntersects {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
      vi.stubGlobal('IntersectionObserver', NeverIntersects)
      try {
        const { tab } = await open(permissions)
        const cards = tab.findAll('[data-stub="VKpiCard"]')
        expect(cards.length).toBeGreaterThan(0)
        const stuck = cards.filter((card) => card.attributes('data-loading') === 'true')
        expect(stuck.map((card) => card.text())).toEqual([])
      } finally {
        vi.unstubAllGlobals()
      }
    },
  )
})
