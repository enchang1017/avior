import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

import type { LineageColumn, LineageNode, LineagePath, LineageRoutine } from './types'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const ColumnPanel = (await import('./ColumnPanel.vue')).default
const RoutinePanel = (await import('./RoutinePanel.vue')).default
const PathPanel = (await import('./PathPanel.vue')).default
const DynamicTray = (await import('./DynamicTray.vue')).default
const LineageToolbar = (await import('./LineageToolbar.vue')).default

const ok = (data: unknown) => ({ data, response: new Response(null, { status: 200 }) })
const snapshot = {
  builtAt: '2026-09-20T09:55:00Z',
  stale: false,
  parsed: 1,
  reused: 0,
  unparsed: 0,
  dynamic: 0,
  opaque: 0,
}

const node = (
  name: string,
  kind: LineageNode['kind'] = 'table',
  extra: Partial<LineageNode> = {},
) => ({ id: `${kind}:${name}`, kind, schema: 'public', name, ...extra }) satisfies LineageNode

const bet = node('bet')
const settle = node('settle_bet', 'function', { arguments: 'bigint', badges: ['dynamic'] })

const column: LineageColumn = {
  state: 'ready',
  snapshot,
  table: bet,
  column: { name: 'status', restricted: true },
  entries: [
    {
      object: settle,
      access: 'write',
      confidence: 'parsed',
      kind: 'routine_write',
      excerpt: 'UPDATE bet',
      line: 3,
    },
    { object: node('bets', 'view'), access: 'read', confidence: 'exact', kind: 'view_read' },
  ],
  counts: [
    { column: { name: 'status', restricted: true }, reads: 8, writes: 3, foreignKey: false },
  ],
  next: 'page-2',
}

function answering(routes: Record<string, (query: Record<string, unknown>) => unknown>) {
  get.mockImplementation(
    (path: string, options: { params: { query: Record<string, unknown> } }) => {
      const reply = routes[path]
      return Promise.resolve(
        reply === undefined
          ? {
              error: { code: 'x', message: `No ${path}.` },
              response: new Response(null, { status: 404 }),
            }
          : ok(reply(options.params.query)),
      )
    },
  )
}

beforeEach(() => get.mockReset())

/** Lets a lazy child's chunk arrive and draw. */
async function settled(): Promise<void> {
  await flushPromises()
  await vi.dynamicImportSettled()
  await flushPromises()
}
afterEach(() => vi.useRealTimers())

describe('ColumnPanel', () => {
  const mountPanel = (name: string | null) =>
    mount(ColumnPanel, {
      props: { instanceId: 'i', database: 'db', table: bet, column: name },
    })

  it('lists the columns with their writes, reads and key marks, and says what it cannot see', async () => {
    answering({
      '/lineage/column': () => ({
        ...column,
        column: undefined,
        summary: { writers: 1, readers: 0, views: 1, triggers: 0, dynamic: 0 },
        counts: [
          { column: { name: 'status', restricted: false }, reads: 8, writes: 3, foreignKey: false },
          {
            column: { name: 'member_id', restricted: false },
            reads: 2,
            writes: 0,
            foreignKey: true,
          },
        ],
      }),
    })
    const panel = mountPanel(null)
    await flushPromises()
    expect(
      get.mock.calls.find(([path]) => path === '/lineage/column')?.[1].params.query,
    ).not.toHaveProperty('column')
    expect(panel.find('[data-testid="impact-summary"]').text()).toBe(
      'public.bet: 1 routine writes it, 1 view depends on it.',
    )
    const rows = panel.findAll('.counts li')
    expect(rows[0]?.find('.tally').text()).toBe('W 3 · R 8')
    expect(rows[0]?.find('.fk').exists()).toBe(false)
    expect(rows[1]?.find('.fk').exists()).toBe(true)
    expect(panel.text()).toContain('Not visible: application writes over its own connection')
    await settled()
    expect(panel.find('[aria-label="Routines that write"]').exists()).toBe(true)
    await panel.find('button.pick').trigger('click')
    expect(panel.emitted('pick')).toEqual([['status']])
  })

  it('says through what a table reads its own column', async () => {
    const policy = {
      object: bet,
      access: 'read' as const,
      confidence: 'exact' as const,
      kind: 'policy_read' as const,
      via: 'policy bet_open',
    }
    answering({
      '/lineage/column': () => ({ ...column, entries: [policy], next: undefined }),
    })
    const panel = mountPanel('status')
    await settled()
    const entry = panel.find('[aria-label="Policies, defaults and keys"] .entry')
    expect(entry.text()).toContain('bet')
    expect(entry.find('.via').text()).toBe('· policy bet_open')
  })

  it('shows who writes and who reads it, badged, counted, and a further page on asking', async () => {
    answering({
      '/lineage/column': (query) =>
        query['cursor'] === 'page-2'
          ? { ...column, entries: [{ ...column.entries?.[0], line: 9 }], next: undefined }
          : column,
    })
    const panel = mountPanel('status')
    await settled()
    const written = panel.find('[aria-label="Routines that write"]')
    expect(written.text()).toContain('settle_bet(bigint)')
    expect(written.text()).toContain('UPDATE bet · line 3')
    expect(written.find('.confidence').text()).toBe('PARSED')
    expect(panel.find('[aria-label="Views"] .confidence').text()).toBe('EXACT')
    expect(panel.find('.counts .tally').text()).toBe('W 3 · R 8')
    expect(panel.findAll('.restricted').length).toBeGreaterThanOrEqual(2)

    await panel
      .findAll('button')
      .find((button) => button.text() === 'Load more')
      ?.trigger('click')
    await settled()
    expect(panel.findAll('[aria-label="Routines that write"] .entry')).toHaveLength(2)
    expect(panel.text()).not.toContain('Load more')

    await written.find('.entry button').trigger('click')
    expect(panel.emitted('open')).toEqual([[settle.id]])
    await panel
      .findAll('button')
      .find((button) => button.text() === 'All columns')
      ?.trigger('click')
    expect(panel.emitted('pick')).toEqual([[null]])
  })

  it('says why when the lineage is refused', async () => {
    answering({})
    const panel = mountPanel('status')
    await flushPromises()
    expect(panel.text()).toContain('No /lineage/column.')
  })

  it('says so while the lineage is building', async () => {
    vi.useFakeTimers()
    answering({ '/schema/columns': () => [], '/lineage/column': () => ({ state: 'building' }) })
    const panel = mountPanel('status')
    await vi.advanceTimersByTimeAsync(0)
    expect(panel.find('.building').text()).toContain('still being built')
    panel.unmount()
  })
})

describe('RoutinePanel', () => {
  const footprint: LineageRoutine = {
    state: 'ready',
    snapshot,
    routine: settle,
    status: 'parsed',
    message: 'syntax error at or near "WITH"',
    writes: [{ table: bet, columns: [{ name: 'amount', restricted: true }], confidence: 'parsed' }],
    reads: [{ table: bet, columns: [{ name: 'id', restricted: false }], confidence: 'exact' }],
    statements: [{ line: 3, excerpt: 'UPDATE bet' }],
    calls: [node('log_event', 'function')],
    transitiveCalls: [node('audit', 'function')],
    triggers: [{ name: 'bet_audit', table: bet, function: node('trg', 'function') }],
    dynamic: [{ line: 5, form: 'EXECUTE', excerpt: 'EXECUTE q' }],
    unmodelled: [{ line: 7, form: 'MERGE' }],
    unresolved: ['tmp_x'],
    mayReachUnknown: true,
  }

  const mountPanel = (routine: LineageNode) =>
    mount(RoutinePanel, {
      props: { instanceId: 'i', database: 'db', routine },
      global: { stubs: { RouterLink: RouterLinkStub } },
    })

  it('shows its reads, writes, statements, calls, triggers and what it builds at run time', async () => {
    answering({ '/lineage/routine': () => footprint })
    const panel = mountPanel(settle)
    await flushPromises()
    expect(panel.find('.touch[data-access="write"]').text()).toContain('amount (restricted)')
    expect(panel.find('.touch[data-access="read"]').text()).toContain('id')
    expect(panel.text()).toContain('UPDATE bet')
    expect(panel.text()).toContain('via audit()')
    expect(panel.text()).toContain('bet_audit on bet')
    expect(panel.text()).toContain('EXECUTE · EXECUTE q')
    expect(panel.text()).toContain('MERGE')
    expect(panel.text()).toContain('tmp_x')
    expect(panel.text()).toContain('syntax error')
    expect(panel.text()).toContain('may reach objects nobody can list')
    expect(panel.find('[data-badge="dynamic"]').text()).toBe('builds SQL at run time')
    await panel.find('.call').trigger('click')
    expect(panel.emitted('open')?.[0]).toEqual([footprint.calls?.[0]?.id])
  })

  it('opens the definition in the object viewer for its kind', () => {
    answering({})
    const link = (routine: LineageNode) =>
      mountPanel(routine).findComponent(RouterLinkStub).props('to')
    expect(link(settle)).toEqual({
      name: 'objects-function',
      query: { database: 'db', schema: 'public', name: 'settle_bet' },
    })
    expect(link(node('close', 'procedure'))).toMatchObject({ name: 'objects-stored-procedure' })
  })

  it('says when it touches nothing', async () => {
    answering({
      '/lineage/routine': () => ({ state: 'ready', snapshot, routine: settle, status: 'opaque' }),
    })
    const panel = mountPanel(settle)
    await flushPromises()
    expect(panel.text()).toContain('It reads and writes no table the parser could name.')
    expect(panel.text()).toContain('It calls no other routine.')
    expect(panel.text()).toContain('Its writes fire no trigger.')
  })
})

describe('PathPanel', () => {
  const path: LineagePath = {
    state: 'ready',
    nodes: [bet, node('member'), node('campaign')],
    routes: [
      {
        nodes: [bet.id, 'table:member', 'table:campaign'],
        edges: [
          {
            from: bet.id,
            to: 'table:member',
            kind: 'foreign_key',
            access: 'reference',
            confidence: 'exact',
          },
          {
            from: 'table:campaign',
            to: 'table:member',
            kind: 'foreign_key',
            access: 'reference',
            confidence: 'exact',
          },
        ],
      },
      {
        nodes: [bet.id, 'table:campaign'],
        edges: [
          {
            from: bet.id,
            to: 'table:campaign',
            kind: 'call',
            access: 'reference',
            confidence: 'parsed',
          },
        ],
      },
    ],
  }

  it('lists the routes with their length and how sure they are, and chooses one', async () => {
    const panel = mount(PathPanel, {
      props: { path, chosen: 0, from: 'public.bet', to: 'public.campaign' },
    })
    const routes = panel.findAll('button.route')
    expect(routes.map((route) => route.text())).toEqual([
      '2bet─FK→member←FK─campaignEXACT',
      '1bet─calls→campaignPARSED',
    ])
    const back = routes[0]?.findAll('.hop-edge')[1]
    expect(back?.attributes('data-against')).toBe('true')
    expect(back?.attributes('title')).toBe("FK, walked against the edge's direction")
    expect(routes[0]?.attributes('aria-pressed')).toBe('true')
    await routes[1]?.trigger('click')
    expect(panel.emitted('choose')).toEqual([[1]])
    expect(panel.text()).toContain('public.bet → public.campaign')
  })

  it('says so when no route joins them', () => {
    const panel = mount(PathPanel, {
      props: { path: { state: 'ready', routes: [] }, chosen: 0, from: 'a', to: 'b' },
    })
    expect(panel.find('.no-route').text()).toContain('No route joins them')
    expect(panel.text()).toContain('a → b')
  })
})

describe('DynamicTray', () => {
  const purge = node('purge', 'function', { arguments: 'text', badges: ['dynamic'] })

  it('folds into a chip that counts them, singular and plural', () => {
    const one = mount(DynamicTray, {
      props: { routines: [settle], total: 1, drawn: new Set<string>() },
    })
    expect(one.find('[data-testid="dynamic-chip"]').text()).toBe('1 dynamic SQL routine')
    const many = mount(DynamicTray, {
      props: { routines: [settle], total: 7, drawn: new Set<string>() },
    })
    expect(many.find('[data-testid="dynamic-chip"]').text()).toBe('7 dynamic SQL routines')
    expect(many.find('[data-testid="dynamic-tray"]').exists()).toBe(false)
  })

  it('opens on the chip, names each routine, and marks only those not drawn', async () => {
    const tray = mount(DynamicTray, {
      props: { routines: [settle, purge], total: 2, drawn: new Set([settle.id]) },
    })
    await tray.find('button.trigger').trigger('click')
    await settled()
    const opened = tray.find('[data-testid="dynamic-tray"]')
    expect(opened.text()).toContain("what it touches can't be known, so it is never drawn")
    expect(opened.text()).not.toContain('None of them is drawn here')
    const rows = opened.findAll('li')
    expect(rows.map((row) => row.find('.routine').text())).toEqual(['settle_bet', 'purge'])
    expect(rows.map((row) => row.find('.off-view').exists())).toEqual([false, true])
    await rows[1]?.find('.routine').trigger('click')
    expect(tray.emitted('open')).toEqual([[purge.id]])
  })
})

describe('LineageToolbar', () => {
  const props = {
    databases: ['db'],
    database: 'db',
    focusRefs: [{ schema: 'public', name: 'bet', kind: 'table' as const }],
    focusValue: 'public.bet',
    toValue: '',
    columnsOf: () => undefined,
    mode: 'neighbourhood' as const,
    hops: 1 as const,
    allowThree: false,
    maxHops: 5,
    kinds: new Set(['table', 'view'] as const),
    confidence: new Set(['exact'] as const),
    extensions: false,
    extensionsHidden: 3,
  }

  it('offers one and two hops until three is allowed', async () => {
    const bar = mount(LineageToolbar, { props })
    expect(bar.findAll('button.hop').map((button) => button.text())).toEqual(['1', '2'])
    await bar.find('button.allow-three').trigger('click')
    expect(bar.emitted('allowThree')).toEqual([[true]])
    await bar.setProps({ allowThree: true, hops: 3 })
    expect(bar.findAll('button.hop').map((button) => button.text())).toEqual(['1', '2', '3'])
    expect(bar.find('button.allow-three').exists()).toBe(false)
    await bar.findAll('button.hop')[1]?.trigger('click')
    expect(bar.emitted('hops')).toEqual([[2]])
  })

  it('toggles kinds, confidence and extension routines', async () => {
    const bar = mount(LineageToolbar, { props })
    const chip = (selector: string) => bar.find(selector)
    expect(chip('[data-kind="table"]').attributes('aria-pressed')).toBe('true')
    expect(chip('[data-kind="function"]').attributes('aria-pressed')).toBe('false')
    await chip('[data-kind="function"]').trigger('click')
    await chip('[data-confidence="parsed"]').trigger('click')
    await chip('button.extensions').trigger('click')
    expect(bar.emitted('kind')).toEqual([['function']])
    expect(bar.emitted('confidence')).toEqual([['parsed']])
    expect(bar.emitted('extensions')).toHaveLength(1)
    expect(chip('button.extensions').text()).toContain('3 hidden')
  })

  it('switches to path mode, where the most hops replace the hop and kind controls', async () => {
    const bar = mount(LineageToolbar, { props })
    const mode = (label: string) => bar.findAll('button.mode').find((one) => one.text() === label)
    await mode('Path to…')?.trigger('click')
    expect(bar.emitted('mode')).toEqual([['path']])
    await bar.setProps({ mode: 'path' })
    await settled()
    expect(bar.findAll('button.hop')).toHaveLength(0)
    const most = bar.findAll('button.max-hops')
    expect(most.map((button) => button.text())).toEqual(['3', '4', '5'])
    await most[0]?.trigger('click')
    expect(bar.emitted('maxHops')).toEqual([[3]])
    expect(bar.text()).toContain('From')
    expect(bar.text()).toContain('To')
  })

  it('keeps only neighbourhood and path in the mode group, and drops hops in the overview', async () => {
    const bar = mount(LineageToolbar, { props })
    expect(bar.findAll('button.mode').map((one) => one.text())).toEqual([
      'Neighbourhood',
      'Path to…',
    ])
    await bar.setProps({ mode: 'overview' })
    expect(bar.findAll('button.mode[aria-pressed="true"]')).toHaveLength(0)
    expect(bar.findAll('button.hop')).toHaveLength(0)
    expect(bar.findAll('button.max-hops')).toHaveLength(0)
  })
})
