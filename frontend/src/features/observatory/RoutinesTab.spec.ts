import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { default: RoutinesTab } = await import('./RoutinesTab.vue')

const ok = (data: unknown) => ({ data, response: { status: 200 } })

const timed = {
  database: 'aviorfixture',
  schema: 'public',
  kind: 'function',
  timed: true,
  notTimedReason: null,
  maxCallMs: null,
}

const routines = {
  from: '2026-09-20T00:00:00Z',
  to: '2026-09-27T00:00:00Z',
  statsSince: '2026-09-03T00:00:00Z',
  tracking: [{ database: 'aviorfixture', since: '2026-09-01T00:00:00Z' }],
  routines: [
    {
      ...timed,
      name: 'settle_bets',
      signature: 'bigint',
      kind: 'procedure',
      calls: 1200,
      totalMs: 96_000,
      selfMs: 40_000,
      meanMs: 80,
      maxCallMs: 2_400,
    },
    {
      ...timed,
      name: 'shop_settle_26_09_25',
      signature: 'bigint',
      calls: 3,
      totalMs: 30,
      selfMs: 30,
      meanMs: 10,
    },
    {
      ...timed,
      name: 'shop_settle_26_09_01',
      signature: 'bigint',
      calls: 2,
      totalMs: 40,
      selfMs: 40,
      meanMs: 20,
    },
    {
      ...timed,
      name: 'lookup_member',
      signature: 'text',
      calls: 0,
      totalMs: 0,
      selfMs: 0,
      meanMs: null,
    },
    {
      ...timed,
      name: 'plain_sql',
      signature: '',
      timed: false,
      notTimedReason: 'LANGUAGE sql needs track_functions = all',
      calls: 0,
      totalMs: 0,
      selfMs: 0,
      meanMs: null,
    },
  ],
  groups: [
    {
      database: 'aviorfixture',
      schema: 'public',
      base: 'shop_settle',
      latest: 'shop_settle_26_09_25',
      versions: [
        { name: 'shop_settle_26_09_25', signature: 'bigint', date: '2026-09-25' },
        { name: 'shop_settle_26_09_01', signature: 'bigint', date: '2026-09-01' },
      ],
    },
  ],
}

const databases = {
  idleDays: 30,
  lastRunAt: null,
  flagsWithheld: null,
  databases: [{ database: 'aviorfixture' }, { database: 'ledger' }],
}

async function open(query: Record<string, string> = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/observatory', name: 'observatory', component: { render: () => null } },
      { path: '/objects/function', name: 'objects-function', component: { render: () => null } },
      {
        path: '/objects/stored-procedure',
        name: 'objects-stored-procedure',
        component: { render: () => null },
      },
    ],
  })
  await router.push({ name: 'observatory', query: { tab: 'routines', ...query } })
  const tab = mount(RoutinesTab, { global: { plugins: [router] } })
  await flushPromises()
  return { tab, router }
}

const routineCalls = () =>
  get.mock.calls
    .filter(([path]) => path === '/observatory/routines')
    .map(([, options]) => (options as { params: { query: unknown } }).params.query)

beforeEach(() => {
  get.mockReset()
  get.mockImplementation((path: string) =>
    Promise.resolve(ok(path === '/observatory/databases' ? databases : routines)),
  )
})

afterEach(() => {
  vi.useRealTimers()
})

describe('the routine timing tab', () => {
  it('asks for seven days by mean per call and lists every routine under the caption', async () => {
    const { tab } = await open()
    expect(routineCalls()).toEqual([{ range: '7d', sort: 'mean' }])
    const caption = tab.get('[data-testid="routines-caption"]').text()
    expect(caption).toContain('Averages over 15-minute snapshots')
    expect(caption).toMatch(/Tracked since (2026-)?09-01 00:00\./u)
    expect(caption).toContain('LANGUAGE sql routines are not timed.')

    const text = tab.get('[data-testid="routines-table"]').text()
    expect(text).toContain('public.settle_bets')
    expect(text).not.toContain('public.settle_bets(bigint)')
    expect(text).toContain('procedure')
    expect(text).toContain('2.4 s')
    expect(text).toMatch(/top-level calls since (2026-)?09-03 00:00/u)
    expect(text).not.toContain('lookup_member')
    expect(tab.get('[data-testid="routines-hidden"]').text()).toBe(
      '2 routines with no calls hidden',
    )
    expect(tab.find('[data-routine-group]').text()).toContain('public.shop_settle')
    expect(tab.find('[data-routine-version]').text()).toContain('latest')
  })

  it('shows a dash for an uncalled routine and leaves a missing slowest call blank', async () => {
    const { tab } = await open({ uncalled: 'shown' })
    expect(tab.get('[data-testid="routines-table"]').text()).toContain(
      'not timed (LANGUAGE sql needs track_functions = all)',
    )
    expect(tab.find('[data-testid="routines-hidden"]').exists()).toBe(false)
    const row = tab
      .findAll('[data-routine-row]')
      .find((node) => node.text().includes('lookup_member'))
    expect(row?.get('[data-cell="mean"]').text()).toBe('—')
    expect(row?.get('[data-cell="max"]').text()).toBe('')
  })

  it('links each routine by its name alone to the definition viewer, the signature as its title', async () => {
    const { tab } = await open()
    const link = tab
      .findAll('[data-routine-link]')
      .find((node) => node.text() === 'public.shop_settle_26_09_25')
    expect(link?.attributes('href')).toContain('/objects/function?')
    expect(link?.attributes('href')).toContain('name=shop_settle_26_09_25')
    expect(link?.attributes('title')).toBe('public.shop_settle_26_09_25(bigint)')
  })

  it('drops the slowest-call column and says why when no routine on the page has one', async () => {
    get.mockImplementation((path: string) =>
      Promise.resolve(
        ok(
          path === '/observatory/databases'
            ? databases
            : {
                ...routines,
                routines: routines.routines.map((one) => ({ ...one, maxCallMs: null })),
              },
        ),
      ),
    )
    const { tab } = await open()
    expect(tab.find('[data-cell="max"]').exists()).toBe(false)
    expect(tab.text()).not.toContain('Slowest top-level call')
    expect(tab.get('[data-testid="routines-no-slowest"]').text()).toContain('none on this page')
  })

  it('drops the database column when one database is chosen, and shows it for all', async () => {
    const all = await open()
    expect(all.tab.get('[data-testid="routines-table"]').text()).toContain('Database')
    const one = await open({ database: 'aviorfixture' })
    const table = one.tab.get('[data-testid="routines-table"]')
    expect(table.findAll('th').map((th) => th.text())).not.toContain('Database')
    expect(table.find('[data-routine-row] td').text()).not.toBe('aviorfixture')
  })

  it('shows routines with no calls when the toggle is cleared, and keeps that in the address', async () => {
    const { tab, router } = await open()
    await tab.get('[data-testid="hide-uncalled"]').setValue(false)
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ uncalled: 'shown' })
    expect(tab.get('[data-testid="routines-table"]').text()).toContain('lookup_member')
  })

  it('sorts by a header, turns round on a second click, and marks the column', async () => {
    const { tab, router } = await open()
    const header = () => tab.get('th[data-sort="calls"]')
    expect(tab.get('th[data-sort="mean"]').attributes('aria-sort')).toBe('descending')
    expect(header().attributes('aria-sort')).toBe('none')
    await header().get('button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ sort: 'calls' })
    expect(router.currentRoute.value.query['order']).toBeUndefined()
    expect(header().attributes('aria-sort')).toBe('descending')
    expect(header().get('[data-sort-mark]').text()).toBe('▼')
    await header().get('button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ sort: 'calls', order: 'asc' })
    expect(header().attributes('aria-sort')).toBe('ascending')
    expect(header().get('[data-sort-mark]').text()).toBe('▲')
    expect(routineCalls().at(-1)).toEqual({ range: '7d', sort: 'calls', order: 'asc' })
  })

  it('restores its choices from the address', async () => {
    await open({ range: '30d', sort: 'calls', database: 'ledger', search: 'settle' })
    expect(routineCalls()).toEqual([
      { range: '30d', sort: 'calls', database: 'ledger', search: 'settle' },
    ])
  })

  it('writes a choice into the address and reads again', async () => {
    const { tab, router } = await open({ search: 'x' })
    await tab.get('th[data-sort="total"] button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ sort: 'total', search: 'x' })
    const database = tab.findAllComponents({ name: 'VSelect' })[1]
    database?.vm.$emit('update:modelValue', 'ledger')
    await flushPromises()
    database?.vm.$emit('update:modelValue', '')
    await flushPromises()
    expect(router.currentRoute.value.query['database']).toBeUndefined()
    expect(routineCalls().at(-1)).toEqual({ range: '7d', sort: 'total', search: 'x' })
  })

  it('waits for the typing to stop before it searches', async () => {
    vi.useFakeTimers()
    const { tab, router } = await open()
    await tab.get('#routines-search').setValue('desp')
    await vi.advanceTimersByTimeAsync(100)
    expect(router.currentRoute.value.query['search']).toBeUndefined()
    await tab.get('#routines-search').setValue('shop_settle')
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(router.currentRoute.value.query['search']).toBe('shop_settle')
    expect(routineCalls().at(-1)).toEqual({ range: '7d', sort: 'mean', search: 'shop_settle' })
    expect(routineCalls()).toHaveLength(2)
  })

  it('says so when nothing matches', async () => {
    get.mockImplementation((path: string) =>
      Promise.resolve(
        ok(path === '/observatory/databases' ? databases : { ...routines, routines: [] }),
      ),
    )
    const { tab } = await open({ search: 'nothing' })
    expect(tab.text()).toContain('No routine matches that name.')
  })
})
