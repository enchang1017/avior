import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { default: LastUsedTab } = await import('./LastUsedTab.vue')
const { default: CapacityTab } = await import('./CapacityTab.vue')
const { default: PerformanceTab } = await import('./PerformanceTab.vue')
const { default: UsageTab } = await import('./UsageTab.vue')

const databases = {
  idleDays: 30,
  lastRunAt: '2026-09-27T09:45:00Z',
  flagsWithheld: null,
  databases: [
    {
      database: 'aviorfixture',
      sizeBytes: 3 * 1024 ** 3,
      sizeBytes7d: 2 * 1024 ** 3,
      sizeBytes30d: null,
      growth: [
        { day: '2026-09-25', sizeBytes: 1 },
        { day: '2026-09-26', sizeBytes: 3 },
      ],
      routinesTrackedSince: '2026-09-27T00:00:00Z',
      relationsTrackedSince: '2026-08-01T00:00:00Z',
      flagged: 2,
      lastResetAt: '2026-09-03T00:00:00Z',
    },
  ],
}

const objects = {
  database: 'aviorfixture',
  idleDays: 30,
  routinesTrackedSince: '2026-09-27T00:00:00Z',
  relationsTrackedSince: '2026-08-01T00:00:00Z',
  flagsWithheld: '1 standby is connected that the observatory does not read; flags are withheld',
  objects: [
    {
      schema: 'public',
      name: 'bet',
      kind: 'table',
      signature: null,
      table: null,
      tracked: true,
      notTrackedReason: null,
      lastUsedAt: '2026-08-20T09:00:00Z',
      lastUsedKind: 'read',
      lastUsedNode: 'primary',
      daysAgo: 38,
      flagged: true,
      constraintBacked: false,
      sizeBytes: 4096,
      growth30dBytes: 1024,
      calls: null,
    },
    {
      schema: 'public',
      name: 'bet_pkey',
      kind: 'index',
      signature: null,
      table: 'public.bet',
      tracked: true,
      notTrackedReason: null,
      lastUsedAt: null,
      lastUsedKind: null,
      lastUsedNode: null,
      daysAgo: null,
      flagged: false,
      constraintBacked: true,
      sizeBytes: 1024,
      growth30dBytes: null,
      calls: null,
    },
  ],
  routineGroups: [
    {
      schema: 'public',
      base: 'shop_settle_order',
      latest: 'shop_settle_order_26_09_25',
      flaggedCount: 1,
      lastUsedAt: null,
      versions: [
        {
          name: 'shop_settle_order_26_09_25',
          signature: 'bigint',
          date: '2026-09-25',
          latest: true,
          lastUsedAt: null,
          daysAgo: null,
          flagged: true,
        },
      ],
    },
  ],
}

const ok = (data: unknown) => ({ data, response: { status: 200 } })

beforeEach(() => {
  get.mockReset()
})

describe('the Last used tab', () => {
  beforeEach(() => {
    get.mockImplementation((path: string) =>
      Promise.resolve(ok(path === '/observatory/databases' ? databases : objects)),
    )
  })

  it('shows each last use as data, the flag, the withheld banner and the columns note', async () => {
    const tab = mount(LastUsedTab)
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/observatory/objects', {
      params: { query: { database: 'aviorfixture' } },
    })
    const text = tab.text()
    expect(text).toContain('last read 2026-08-20 (38 days ago)')
    const flag = tab.find('[data-flag]')
    expect(flag.text()).toContain('idle > 30 days')
    expect(flag.attributes('title')).toBe('flagged: last use more than 30 days ago')
    expect(flag.find('.sr-only').text()).toBe('flagged: last use more than 30 days ago')
    expect(text).toContain('enforces a constraint')
    expect(text).toContain('flags are withheld')
    expect(tab.find('[data-testid="columns-note"]').text()).toBe(
      'Columns are not listed: PostgreSQL keeps no per-column usage statistics.',
    )
    expect(tab.find('[data-routine-group]').text()).toContain('public.shop_settle_order')
    expect(tab.find('[data-routine-group]').text()).toContain('1 version · 1 flagged')
    expect(tab.findAll('[data-routine-version]')).toHaveLength(1)
    expect(tab.find('[data-routine-version]').text()).toContain('latest')
    expect(text).not.toMatch(/dead|unused|obsolete|deprecated/iu)
  })
})

describe('the Capacity tab', () => {
  it('draws size, growth and a sparkline per database', async () => {
    get.mockResolvedValue(ok(databases))
    const tab = mount(CapacityTab)
    await flushPromises()
    expect(tab.text()).toContain('3.0 GB')
    expect(tab.text()).toContain('+1.0 GB')
    expect(tab.text()).toContain('2 flagged')
    expect(tab.find('svg.sparkline').attributes('data-trend')).toBe('rising')
  })
})

describe('the Performance tab', () => {
  it('shows the reason when the statements cannot be read', async () => {
    get.mockImplementation((path: string) => {
      if (path === '/observatory/statements') {
        return Promise.resolve({
          error: { code: 'observatory.unavailable', message: 'pg_stat_statements is missing.' },
          response: { status: 409 },
        })
      }
      if (path === '/observatory/databases') return Promise.resolve(ok(databases))
      if (path === '/observatory/activity') {
        return Promise.resolve(ok({ generatedAt: '2026-09-27T10:00:00Z', backends: [] }))
      }
      return Promise.resolve(ok({ database: 'aviorfixture', tables: [] }))
    })
    const tab = mount(PerformanceTab)
    await flushPromises()
    expect(tab.find('[data-testid="statements-failure"]').text()).toBe(
      'pg_stat_statements is missing.',
    )
    expect(tab.text()).toContain('Nothing has been running that long.')
  })
})

describe('the Performance tab statements', () => {
  const statementCalls = () =>
    get.mock.calls.filter(([path]) => path === '/observatory/statements') as [
      string,
      { params: { query: { includeOwn?: boolean } } },
    ][]

  it("asks for Avior's own monitoring only once the toggle is on", async () => {
    get.mockImplementation((path: string) => {
      if (path === '/observatory/databases') return Promise.resolve(ok(databases))
      if (path === '/observatory/statements') {
        return Promise.resolve(ok({ statsSince: '2026-09-01T07:20:48.459Z', statements: [] }))
      }
      if (path === '/observatory/activity') {
        return Promise.resolve(ok({ generatedAt: '2026-09-27T10:00:00Z', backends: [] }))
      }
      return Promise.resolve(ok({ database: 'aviorfixture', tables: [] }))
    })
    const tab = mount(PerformanceTab)
    await flushPromises()
    const toggle = tab.find<HTMLInputElement>('[data-testid="include-own"]')
    expect(toggle.element.checked).toBe(false)
    expect(statementCalls()).toHaveLength(1)
    expect(statementCalls()[0]?.[1].params.query.includeOwn).not.toBe(true)
    expect(tab.text()).toContain('Cumulative since 09-01 07:20.')
    expect(tab.find('time').attributes('title')).toBe('2026-09-01T07:20:48.459Z')

    await toggle.setValue(true)
    await flushPromises()
    expect(statementCalls()).toHaveLength(2)
    expect(statementCalls()[1]?.[1].params.query.includeOwn).toBe(true)

    await toggle.setValue(false)
    await flushPromises()
    expect(statementCalls()[2]?.[1].params.query.includeOwn).not.toBe(true)
  })
})

describe('the Avior usage tab', () => {
  it('asks for the last seven days and links each row to the audit log', async () => {
    get.mockResolvedValue(
      ok({
        from: '2026-09-20T00:00:00Z',
        to: '2026-09-27T00:00:00Z',
        rows: [
          {
            operatorId: '00000000-0000-0000-0000-000000000001',
            operator: 'lin.wei',
            database: 'aviorfixture',
            statements: 3,
            allowed: 2,
            refused: 1,
            refusals: [{ reason: 'wildcard_select', count: 1 }],
            executed: 2,
            elapsedP50Ms: 5,
            elapsedP95Ms: null,
            rowsTotal: 10,
          },
        ],
      }),
    )
    const tab = mount(UsageTab, {
      props: { scope: 'all', auditLog: 'all' },
      global: { stubs: { RouterLink: { template: '<a class="text-primary"><slot /></a>' } } },
    })
    await flushPromises()
    const [, options] = get.mock.calls[0] as [string, { params: { query: { from: string } } }]
    expect(options.params.query.from).toMatch(/Z$/u)
    expect(tab.text()).toContain('wildcard_select ×1')
    expect(tab.text()).toContain('Audit log')
  })
})
