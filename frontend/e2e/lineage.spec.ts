import { expect, test, type Locator, type Page } from '@playwright/test'

import { answer, answerJson, calls, record } from './redesign/fixtures'
import { serveCapLineage } from './perf/scene'
import { administrator, serveFixedApi } from './visual/api'
import { roundTwoHops } from './visual/aviorlineage'
import { addressOf, betTables, lineageGet, nodes } from './visual/lineage'

/*
 * FEAT-070's page, AC-28 to AC-40, against the fixed API: the lineage answers
 * come from a small fixture server over the study's `public.bet`
 * neighbourhood, so the page is driven the way an operator drives it.
 */

test.use({ viewport: { width: 1440, height: 1000 }, timezoneId: 'UTC', locale: 'en-US' })

const chart = (page: Page) => page.getByTestId('lineage-chart')
const is = (name: string, value: string, match = '=') => `[${name}${match}${JSON.stringify(value)}]`
const star = (page: Page, id: string) => chart(page).locator(`[data-node]${is('data-id', id)}`)
const edges = (page: Page) => chart(page).locator('[data-edge]')
const edge = (page: Page, key: string) => chart(page).locator(is('data-edge-key', key))
const openBetCountRead = `${nodes.openBetCount.id}>${nodes.bet.id}:routine_read:read`

type Node = (typeof nodes)[keyof typeof nodes]

function at(focus: Node, extra: Record<string, string> = {}): string {
  const query = new URLSearchParams({ database: 'aviorfixture', ...addressOf(focus), ...extra })
  return `/lineage?${query.toString()}`
}

/**
 * Points at an edge near its middle, on a dash: a parsed edge is drawn 5 on and
 * 4 off, and the browser does not hit-test the gaps, so the middle of the line
 * can be empty space.
 */
async function hoverEdge(page: Page, key: string): Promise<void> {
  const point = await edge(page, key).evaluate((element) => {
    const line = element as SVGPathElement
    const matrix = line.getScreenCTM()
    if (matrix === null) return null
    const length = line.getTotalLength()
    const scene = line.getPointAtLength(Math.floor(length / 2 / 9) * 9 + 2.5)
    const screen = new DOMPoint(scene.x, scene.y).matrixTransform(matrix)
    return { x: screen.x, y: screen.y }
  })
  if (point === null) throw new Error(`edge ${key} is not drawn`)
  await page.mouse.move(point.x, point.y)
}

async function openBet(page: Page, extra: Record<string, string> = {}): Promise<void> {
  await page.goto(at(nodes.bet, extra))
  await expect(star(page, nodes.bet.id)).toHaveAttribute('data-tier', 'focus', { timeout: 15_000 })
}

async function choose(page: Page, control: string, option: string): Promise<void> {
  await page.locator(control).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

const addressed = (page: Page) => {
  const query = new URL(page.url()).searchParams
  return ['schema', 'name', 'kind', 'signature'].map((key) => query.get(key))
}

const opacity = (locator: Locator) =>
  locator.evaluate((element) => Number(getComputedStyle(element).opacity))

const strokeOf = (locator: Locator) =>
  locator.evaluate((element) => getComputedStyle(element).stroke)

function token(page: Page, name: string): Promise<string> {
  return page.evaluate((property) => {
    const probe = document.createElement('i')
    probe.style.color = `var(${property})`
    document.body.append(probe)
    const colour = getComputedStyle(probe).color
    probe.remove()
    return colour
  }, name)
}

async function serveLineage(page: Page): Promise<void> {
  await serveFixedApi(page)
  await answerJson(page, '/schema/tables', betTables)
}

test.beforeEach(async ({ page }) => {
  await serveLineage(page)
})

test('the rail opens lineage on the database and object in the address', async ({ page }) => {
  await page.goto('/')
  const entry = page.locator('.rail').getByRole('link', { name: 'Lineage', exact: true })
  await expect(entry).toBeVisible()
  const labels = await page
    .locator('.rail nav > ul > li:not(.edge)')
    .evaluateAll((items) =>
      items.map(
        (item) =>
          item.querySelector('a, button')?.getAttribute('title') ?? item.textContent?.trim() ?? '',
      ),
    )
  const place = (label: string) => labels.findIndex((text) => text.startsWith(label))
  expect(place('Lineage')).toBeGreaterThan(0)
  expect(place('Lineage')).toBe(place('Database Objects') + 1)

  await entry.click()
  await expect(page).toHaveURL(/\/lineage$/u)
  await expect(page.getByTestId('lineage-page')).toContainText('Pick a database and an object')

  await openBet(page)
  await expect(page.locator('#lineage-database')).toHaveValue('aviorfixture')
  await expect(page.locator('#lineage-focus')).toHaveValue('public.bet')
  await expect(page.getByTestId('column-panel')).toContainText('public.bet')

  await choose(page, '#lineage-focus', 'public.member')
  await expect.poll(() => addressed(page)).toEqual(['public', 'member', 'table', null])
  await expect(star(page, nodes.member.id)).toHaveAttribute('data-tier', 'focus')
})

test('the hud target follows the lineage focus', async ({ page }) => {
  await openBet(page)
  const readout = page.locator('.hud-bar .target .aimed')
  await expect(readout).toHaveText(`${administrator.instance.name} / aviorfixture / public.bet`)
  await choose(page, '#lineage-focus', 'public.member')
  await expect(readout).toHaveText(`${administrator.instance.name} / aviorfixture / public.member`)
})

test('the first hop is drawn in full and the rest dimmed', async ({ page }) => {
  await openBet(page, { hops: '2' })
  await expect(star(page, nodes.member.id)).toHaveAttribute('data-tier', 'near')
  await expect(star(page, nodes.account.id)).toHaveAttribute('data-tier', 'far')
  await page.waitForTimeout(700)
  expect(await opacity(star(page, nodes.bet.id))).toBe(1)
  expect(await opacity(star(page, nodes.member.id))).toBe(1)
  expect(await opacity(star(page, nodes.account.id))).toBeCloseTo(0.35, 2)
  const far = edge(page, `${nodes.account.id}>${nodes.member.id}:foreign_key:reference`)
  expect(await opacity(far)).toBeCloseTo(0.35, 2)
  const near = edge(page, `${nodes.bet.id}>${nodes.member.id}:foreign_key:reference`)
  expect(await opacity(near)).toBe(1)
})

test('hovering a node lights only its edges', async ({ page }) => {
  await openBet(page, { hops: '2' })
  const hovered = nodes.memberBets.id
  const untouched = edge(page, `${nodes.settleBet.id}>${nodes.bet.id}:routine_write:write`)
  const before = await strokeOf(untouched)
  await star(page, hovered).hover()

  const lit = chart(page).locator('[data-edge][data-lit="true"]')
  const keys = await lit.evaluateAll((all) =>
    all.map((line) => line.getAttribute('data-edge-key') ?? ''),
  )
  expect(keys).toHaveLength(4)
  for (const key of keys) expect(key).toContain(hovered)
  const target = await token(page, '--color-target')
  for (const line of await lit.all()) expect(await strokeOf(line)).toBe(target)
  expect(await strokeOf(untouched)).toBe(before)
  await expect(star(page, nodes.bet.id)).toHaveAttribute('data-tier', 'focus')

  await page.mouse.move(5, 995)
  await expect(lit).toHaveCount(0)
})

test('read and write are the only edge colours and parsed edges are dashed', async ({ page }) => {
  await openBet(page, { hops: '2' })
  const read = await token(page, '--color-lineage-read')
  const write = await token(page, '--color-lineage-write')
  const neutral = await token(page, '--color-chart-edge-near')
  const drawn = await edges(page).evaluateAll((all) =>
    all.map((line) => ({
      tone: line.getAttribute('data-tone'),
      confidence: line.getAttribute('data-confidence'),
      mark: line.getAttribute('data-mark'),
      stroke: getComputedStyle(line).stroke,
      dash: getComputedStyle(line).strokeDasharray,
      mid: line.getAttribute('marker-mid'),
    })),
  )
  expect(new Set(drawn.map((line) => line.stroke))).toEqual(new Set([read, write, neutral]))
  for (const line of drawn) {
    expect(line.stroke).toBe(line.tone === 'read' ? read : line.tone === 'write' ? write : neutral)
    expect(line.dash === 'none', `${line.confidence} dashed ${line.dash}`).toBe(
      line.confidence === 'exact',
    )
    expect(line.mid).toBe(line.mark === null ? 'none' : `url(#lineage-mark-${line.mark})`)
  }
  expect(drawn.filter((line) => line.mark === 'foreign_key').length).toBeGreaterThan(0)
  expect(drawn.filter((line) => line.mark === 'trigger')).toHaveLength(2)

  await hoverEdge(page, openBetCountRead)
  await expect(page.getByTestId('lineage-hover-label')).toHaveText('PARSED · routine read · read')
  await expect(page.getByRole('list', { name: 'Legend' })).toContainText('PARSED')
})

test('a column opens the panel with badges, counts and what is not visible', async ({ page }) => {
  await openBet(page)
  const panel = page.getByTestId('column-panel')
  const notVisible = 'Not visible: application writes over its own connection'
  await expect(panel).toContainText(notVisible)
  await panel.locator('.counts').getByRole('button', { name: 'status', exact: true }).click()
  await expect(page).toHaveURL(/column=status/u)
  await expect(chart(page).locator('[data-edge][data-highlighted="true"]')).toHaveCount(4)

  const written = panel.getByRole('region', { name: 'Routines that write' })
  const read = panel.getByRole('region', { name: 'Routines that read' })
  const views = panel.getByRole('region', { name: 'Views' })
  await expect(written.locator('.entry')).toHaveCount(4)
  await expect(written.locator('.confidence')).toHaveText(['PARSED', 'PARSED', 'PARSED', 'DYNAMIC'])
  await expect(written).toContainText("UPDATE bet SET status = 'settled' · line 14")
  await expect(read.locator('.confidence')).toHaveText(['PARSED', 'PARSED', 'PARSED'])
  await expect(views.locator('.confidence')).toHaveText(['EXACT', 'EXACT'])

  const status = panel.locator('.counts li', { hasText: 'status' })
  await expect(status.locator('.tally')).toHaveText('W 3 · R 8')
  await expect(
    panel.locator('.counts li', { hasText: 'payout' }).locator('.restricted'),
  ).toBeVisible()
  await expect(panel).toContainText(notVisible)

  await panel.locator('.counts').getByRole('button', { name: 'id', exact: true }).click()
  await expect(page).toHaveURL(/column=id/u)
  await expect(panel).toContainText('Nothing the database holds reads or writes it.')
  await expect(panel).toContainText(notVisible)
})

test('a typed table.column opens straight onto the column', async ({ page }) => {
  await openBet(page)
  const picker = page.locator('#lineage-focus')
  await picker.fill('bet.st')
  await expect(page.getByRole('option').filter({ hasText: 'public.bet.' })).toHaveText([
    'public.bet.stake',
    'public.bet.status',
  ])
  await page.getByRole('option', { name: 'public.bet.status', exact: true }).click()
  await expect(page).toHaveURL(/column=status/u)
  const panel = page.getByTestId('column-panel')
  await expect(panel.getByRole('heading', { level: 2 })).toHaveText('public.bet.status')
  await expect(chart(page).locator('[data-edge][data-highlighted="true"]')).toHaveCount(4)
  const dimming = await chart(page)
    .locator('[data-edge]')
    .evaluateAll((edges) =>
      edges.map((one) => [
        one.getAttribute('data-highlighted') === 'true',
        one.classList.contains('far'),
      ]),
    )
  expect(dimming.length, 'the chart draws edges off the column too').toBeGreaterThan(4)
  for (const [lit, dimmed] of dimming) expect(dimmed, 'an edge off the column is dimmed').toBe(!lit)
  await expect(picker).toHaveValue('public.bet.status')

  await picker.fill('public.bet.payout')
  await picker.press('Enter')
  await expect(page).toHaveURL(/column=payout/u)

  await picker.fill('public.bet.nothing_here')
  await picker.press('Enter')
  await expect(
    page.getByRole('status').filter({ hasText: 'No object or column by that name' }),
  ).toBeVisible()
  await expect(page).toHaveURL(/column=payout/u)

  await openBet(page, { column: 'status' })
  await expect(panel.getByTestId('impact-summary')).toContainText('public.bet.status:')
  await expect(chart(page).locator('[data-edge][data-highlighted="true"]')).toHaveCount(4)
})

test('the column list shows writes, reads and key markers before a column is picked', async ({
  page,
}) => {
  await openBet(page)
  const panel = page.getByTestId('column-panel')
  const row = (name: string) =>
    panel.locator('.counts li').filter({ has: page.getByRole('button', { name, exact: true }) })
  const memberId = row('member_id')
  await expect(memberId.locator('.tally')).toHaveText('W 0 · R 8')
  await expect(memberId.locator('.fk')).toBeVisible()
  const status = row('status')
  await expect(status.locator('.tally')).toHaveText('W 3 · R 8')
  await expect(status.locator('.fk')).toHaveCount(0)
  await expect(page).not.toHaveURL(/column=/u)
})

test('the panel opens with the impact in one sentence and the entries grouped by kind', async ({
  page,
}) => {
  await openBet(page)
  const panel = page.getByTestId('column-panel')
  await expect(panel.getByTestId('impact-summary')).toHaveText(
    'public.bet: 4 routines write it, 5 read it, 3 views depend on it, 2 triggers; 1 DYNAMIC routine may reach it.',
  )
  await expect(panel.getByRole('region', { name: 'Routines that write' })).toBeVisible()
  await expect(panel.getByRole('region', { name: 'Triggers' })).toContainText('bet_defaults')
  await panel.getByRole('button', { name: /^status/u }).click()
  await expect(panel.getByTestId('impact-summary')).toHaveText(
    'public.bet.status: 3 routines write it, 3 read it, 2 views depend on it, 1 trigger; 1 DYNAMIC routine may reach it.',
  )
  await expect(
    panel.getByRole('region', { name: 'Routines that write' }).locator('.count'),
  ).toHaveText('4 routines')
  await expect(panel.locator('.usage:not(.indirect) h3')).toHaveText([
    /^Routines that write/u,
    /^Routines that read/u,
    /^Views/u,
  ])
  await expect(panel.locator('.usage .entry').first().locator('.confidence')).toBeVisible()
  const triggers = panel.getByRole('region', { name: 'Triggers' })
  await expect(triggers).toContainText('bet_status_audit')
  await expect(triggers).toContainText('runs trg_bet_audit()')
  await expect(triggers).toContainText('update')
})

test('indirect impact is listed apart with its chain and its cap', async ({ page }) => {
  await openBet(page, { column: 'status' })
  const panel = page.getByTestId('column-panel')
  const indirect = panel.getByRole('region', { name: 'Indirect' })
  await expect(indirect.locator('.entry')).toHaveCount(2)
  await expect(indirect.locator('.chain').first()).toHaveText('via view public.member_bets')
  await expect(indirect.locator('.note')).toHaveCount(0)

  await panel.locator('.counts').getByRole('button', { name: 'stake', exact: true }).click()
  await expect(page).toHaveURL(/column=stake/u)
  await expect(indirect.locator('.entry')).toHaveCount(20)
  await expect(indirect.locator('.note')).toHaveText('20 of 26 shown.')
})

test('copy as markdown copies the summary and the groups', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await openBet(page, { column: 'status' })
  const panel = page.getByTestId('column-panel')
  await panel.getByRole('button', { name: 'Copy as Markdown' }).click()
  await expect(panel.getByRole('status')).toHaveText('Copied as Markdown.')
  const copied = await page.evaluate(() => navigator.clipboard.readText())
  expect(copied.startsWith('public.bet.status: 3 routines write it')).toBe(true)
  for (const heading of ['### Routines that write', '### Views', '### Triggers', '### Indirect'])
    expect(copied).toContain(heading)
  expect(copied).toContain(
    "`public.settle_bet(bigint, numeric)` — function · PARSED · UPDATE bet SET status = 'settled' · line 14",
  )
  expect(copied).toContain('via view public.member_bets')
})

test('dynamic routines are listed in the tray and never drawn', async ({ page }) => {
  await openBet(page)
  const chip = page.getByTestId('dynamic-chip')
  const tray = page.getByTestId('dynamic-tray')
  await expect(chip).toHaveText('2 dynamic SQL routines')
  await expect(tray).toHaveCount(0)
  await chip.click()
  await expect(tray).toContainText("what it touches can't be known, so it is never drawn")
  await expect(tray).not.toContainText('None of them is drawn here')
  const offView = (name: string) => tray.locator('li', { hasText: name }).locator('.off-view')
  await expect(tray.locator('.routine')).toHaveText(['purge_table', 'adv_const_exec'])
  await expect(offView('purge_table')).toHaveCount(0)
  await expect(offView('adv_const_exec')).toHaveText('not in current view')

  const purge = star(page, nodes.purgeTable.id)
  await expect(purge).toHaveCount(1)
  await expect(chart(page).locator(`.star-badge${is('data-for', nodes.purgeTable.id)}`)).toHaveText(
    'DYNAMIC',
  )
  const fromPurge = await chart(page)
    .locator(is('data-edge-key', `${nodes.purgeTable.id}>`, '^='))
    .evaluateAll((all) => all.map((line) => line.getAttribute('data-edge-key')))
  expect(fromPurge).toEqual([`${nodes.purgeTable.id}>${nodes.bet.id}:routine_read:read`])
  await expect(
    chart(page).locator(is('data-edge-key', `>${nodes.purgeTable.id}:`, '*=')),
  ).toHaveCount(0)
  await expect(
    chart(page).locator(`.star-badge${is('data-for', nodes.closeMember.id)}`),
  ).toHaveText('MAY REACH UNKNOWN')
  await expect(chart(page)).toHaveAttribute('aria-label', /1 routine builds SQL at run time/u)

  await page.getByRole('group', { name: 'Hops' }).getByRole('button', { name: '2' }).click()
  await expect(star(page, nodes.constExec.id)).toHaveAttribute('data-tier', 'far')
  await expect(
    edge(page, `${nodes.closeMember.id}>${nodes.constExec.id}:call:reference`),
  ).toHaveCount(1)
  await chip.click()
  await expect(offView('adv_const_exec')).toHaveCount(0)
  await tray.locator('.routine', { hasText: 'adv_const_exec' }).click()
  await expect(star(page, nodes.constExec.id)).toHaveAttribute('data-tier', 'focus')
})

test('a collapsed hub expands on click', async ({ page }) => {
  const seen = record(page)
  await openBet(page, { hops: '2' })
  const hub = star(page, nodes.ledger.id)
  await expect(hub).toHaveAttribute('data-hidden', '31')
  await expect(chart(page).locator('text.label', { hasText: 'ledger +31' })).toHaveCount(1)
  await expect(chart(page).locator('[data-node][data-id*="ledger_entry"]')).toHaveCount(0)

  await hub.click({ force: true })
  await expect(chart(page).locator('[data-node][data-id*="ledger_entry"]')).toHaveCount(6)
  await expect(hub).not.toHaveAttribute('data-hidden', /.+/u)
  const expanded = calls(seen, 'GET', '/lineage/graph').flatMap((call) => {
    const hubId = call.query.get('expand')
    return hubId === null ? [] : [hubId]
  })
  expect(expanded).toEqual([nodes.ledger.id])
  await expect(star(page, nodes.bet.id)).toHaveAttribute('data-tier', 'focus')
})

test('extension routines appear only with the toggle', async ({ page }) => {
  const seen = record(page)
  await openBet(page)
  await expect(star(page, nodes.similarity.id)).toHaveCount(0)
  const toggle = page.locator('button.extensions')
  await expect(toggle).toHaveAttribute('aria-pressed', 'false')
  await expect(toggle).toContainText('2 hidden')

  await toggle.click()
  await expect(star(page, nodes.similarity.id)).toHaveCount(1)
  await expect(star(page, nodes.wordSimilarity.id)).toHaveCount(1)
  const asked = calls(seen, 'GET', '/lineage/graph').map((call) => call.query.get('extensions'))
  expect(asked).toEqual([null, 'true'])

  await toggle.click()
  await expect(star(page, nodes.similarity.id)).toHaveCount(0)
})

test('three hops appear only after opting in', async ({ page }) => {
  const seen = record(page)
  await openBet(page)
  const hops = page.getByRole('group', { name: 'Hops' }).locator('button.hop')
  await expect(hops).toHaveText(['1', '2'])

  await hops.filter({ hasText: '2' }).click()
  await expect(page).toHaveURL(/hops=2/u)
  await expect(star(page, nodes.account.id)).toHaveAttribute('data-tier', 'far')

  await page.getByRole('button', { name: 'Allow 3 hops' }).click()
  await expect(hops).toHaveText(['1', '2', '3'])
  await hops.filter({ hasText: '3' }).click()
  await expect(page).toHaveURL(/hops=3/u)
  await expect(hops.filter({ hasText: '3' })).toHaveAttribute('aria-pressed', 'true')
  await expect(star(page, nodes.bet.id)).toHaveAttribute('data-tier', 'focus')
  await expect
    .poll(() => calls(seen, 'GET', '/lineage/graph').filter((c) => c.query.get('hops') === '3'))
    .toHaveLength(1)
  for (const call of calls(seen, 'GET', '/lineage/graph')) {
    expect(call.query.get('cluster')).toBe(call.query.get('hops') === '3' ? 'true' : null)
  }

  await page.goto(at(nodes.bet, { hops: '3' }))
  await expect(page.getByRole('button', { name: 'Allow 3 hops' })).toHaveCount(0)
})

type Played = { iterations: number; state: string }

function traces(page: Page): Promise<Played[]> {
  return page.evaluate(() =>
    document
      .getAnimations()
      .filter(
        (animation) =>
          animation instanceof CSSAnimation && animation.animationName.startsWith('path-trace'),
      )
      .map((animation) => ({
        iterations: animation.effect?.getComputedTiming().iterations ?? 0,
        state: animation.playState,
      })),
  )
}

const stillTracing = async (page: Page) =>
  (await traces(page)).filter((played) => played.state === 'running').length

test('path mode draws the routes and traces the chosen one once', async ({ page }) => {
  const seen = record(page)
  await openBet(page)
  await page.getByRole('button', { name: 'Path to…' }).click()
  await expect(page).toHaveURL(/mode=path/u)
  await expect(page.getByTestId('lineage-page')).toContainText(
    'Choose the object to trace a route to.',
  )
  await choose(page, '#lineage-to', 'promo.campaign')

  const routes = page.getByTestId('path-panel').locator('button.route')
  await expect(routes.first()).toHaveAttribute('aria-pressed', 'true')
  const count = await routes.count()
  expect(count).toBeGreaterThanOrEqual(2)
  expect(count).toBeLessThanOrEqual(4)
  const asked = calls(seen, 'GET', '/lineage/path').at(-1)
  expect(['fromSchema', 'fromName', 'fromKind'].map((key) => asked?.query.get(key))).toEqual([
    'public',
    'bet',
    'table',
  ])
  expect(['toSchema', 'toName', 'toKind'].map((key) => asked?.query.get(key))).toEqual([
    'promo',
    'campaign',
    'table',
  ])
  expect(asked?.query.get('from')).toBeNull()
  expect(asked?.query.get('maxHops')).toBe('5')

  const traced = chart(page).locator('[data-edge][data-step]')
  await expect(traced.first()).toBeAttached()
  const first = await traces(page)
  expect(first).toHaveLength(await traced.count())
  for (const played of first) expect(played.iterations).toBe(1)
  await expect.poll(() => stillTracing(page)).toBe(0)

  await routes.nth(1).click()
  await expect(routes.nth(1)).toHaveAttribute('aria-pressed', 'true')
  const again = await traces(page)
  expect(again.length).toBeGreaterThan(0)
  for (const played of again) expect(played.iterations).toBe(1)
  await expect.poll(() => stillTracing(page)).toBe(0)
})

test('a chosen route dims everything off it and each row reads as a chain', async ({ page }) => {
  await openBet(page)
  await page.getByRole('button', { name: 'Path to…' }).click()
  await choose(page, '#lineage-to', 'promo.campaign')
  const routes = page.getByTestId('path-panel').locator('button.route')
  await expect(routes.first()).toHaveAttribute('aria-pressed', 'true')

  const tiersMatchTheRoute = async () => {
    const onRoute = await chart(page)
      .locator('[data-edge][data-step]')
      .evaluateAll((lines) =>
        lines.flatMap((line) => {
          const [from = '', rest = ''] = (line.getAttribute('data-edge-key') ?? '').split('>')
          return [from, rest.split(':').slice(0, -2).join(':')]
        }),
      )
    const stars = await chart(page)
      .locator('[data-node]')
      .evaluateAll((all) =>
        all.map((one) => [one.getAttribute('data-id'), one.getAttribute('data-tier')]),
      )
    for (const [id, tier] of stars) {
      const expected = tier === 'focus' ? 'focus' : onRoute.includes(id ?? '') ? 'near' : 'far'
      expect(tier, `${id} against the chosen route`).toBe(expected)
    }
    expect(
      stars.some(([, tier]) => tier === 'far'),
      'something is off the route',
    ).toBe(true)
    const offRoute = chart(page).locator('[data-edge]:not([data-step])').first()
    await expect(offRoute).toHaveClass(/\bfar\b/u)
  }
  await tiersMatchTheRoute()
  await routes.nth(1).click()
  await expect(routes.nth(1)).toHaveAttribute('aria-pressed', 'true')
  await tiersMatchTheRoute()

  const row = routes.first()
  await expect(row.locator('.hop-node').first()).toHaveText('bet')
  await expect(row.locator('.hop-node').last()).toHaveText('campaign')
  await expect(row.locator('.hop-edge').first()).toHaveText(/^(─.+→|←.+─)$/u)
  const box = await row
    .locator('.chain')
    .evaluate((chain) => chain.scrollWidth <= chain.clientWidth)
  expect(box, 'the chain is shown whole, never cut').toBe(true)
})

test('a chosen route explains itself hop by hop', async ({ page }) => {
  await openBet(page)
  await page.getByRole('button', { name: 'Path to…' }).click()
  await choose(page, '#lineage-to', 'promo.campaign')
  const detail = page.getByTestId('route-detail')
  await expect(detail.locator('.summary')).toHaveText(
    /^Changing campaign's columns affects bet, through \d hops \(.+\), (all EXACT|at best PARSED)\.$/u,
  )
  await expect(detail.locator('.evidence', { hasText: 'bet.member_id → member.id' })).toHaveCount(1)
  await expect(chart(page).locator('[data-edge][data-step]').first()).not.toHaveAttribute('d', /Q/u)

  const routes = page.getByTestId('path-panel').locator('button.route')
  let statement = false
  for (let index = 0; index < (await routes.count()) && !statement; index += 1) {
    await routes.nth(index).click()
    statement = (await detail.locator('.evidence', { hasText: 'line 12' }).count()) === 1
  }
  expect(statement, "a routine's hop shows its statement and line").toBe(true)

  const hop = detail.locator('.hop').first()
  await hop.click()
  await expect(hop).toHaveAttribute('aria-pressed', 'true')
  const picked = chart(page).locator('[data-edge][data-selected="true"]')
  await expect(picked).toHaveCount(1)
  const key = (await picked.getAttribute('data-edge-key')) ?? ''
  await expect(chart(page).locator(`[data-emphasis=${JSON.stringify(key)}]`)).toHaveAttribute(
    'stroke-width',
    '3',
  )
  await expect(chart(page).locator('[data-node].lit')).toHaveCount(2)

  await hop.locator('.hop-name').last().click()
  await expect(page).not.toHaveURL(/mode=path/u)
  await expect(chart(page).locator('[data-node][data-tier="focus"]')).toHaveCount(1)
})

test('the chart is a galaxy: arcs and halos, stars by magnitude and temperature', async ({
  page,
}) => {
  await openBet(page, { hops: '2' })
  const svg = chart(page)
  await expect(svg.locator('.halos path')).toHaveCount(6)
  const drawn = await svg.locator('[data-edge]').evaluateAll((all) =>
    all.map((line) => ({
      d: line.getAttribute('d') ?? '',
      weight: Number(line.getAttribute('stroke-width')),
      tone: line.getAttribute('data-tone'),
      confidence: line.getAttribute('data-confidence'),
    })),
  )
  expect(
    drawn.every((line) => line.d.includes('Q')),
    'every edge curves around the focus',
  ).toBe(true)
  const weight = (tone: string, confidence: string) =>
    drawn.find((line) => line.tone === tone && line.confidence === confidence)?.weight ?? 0
  expect(weight('write', 'parsed')).toBeGreaterThan(weight('read', 'parsed'))
  expect(weight('read', 'exact')).toBeGreaterThan(weight('read', 'parsed'))

  await expect(star(page, nodes.memberBets.id)).toHaveClass(/stroke-star-view/u)
  await expect(star(page, nodes.settleBet.id)).toHaveClass(/fill-star-routine/u)
  await expect(star(page, nodes.member.id)).toHaveClass(/fill-star-table/u)
  const radii = await svg
    .locator('circle[data-node][data-tier="near"]')
    .evaluateAll((all) => all.map((one) => Number(one.getAttribute('r'))))
  expect(new Set(radii).size, 'stars are sized by how connected they are').toBeGreaterThan(1)
  expect(await svg.locator('ellipse').count(), 'a ring per hop').toBeGreaterThanOrEqual(2)

  await hoverEdge(page, openBetCountRead)
  await expect(svg.locator(`[data-emphasis=${JSON.stringify(openBetCountRead)}]`)).toHaveAttribute(
    'stroke-width',
    '3',
  )
  await expect(page.locator('[data-twinkle]'), 'the chart draws no twinkle').toHaveCount(0)
})

const galaxy = (page: Page) => page.getByTestId('lineage-galaxy')

test('zooming out or the overview control shows the galaxy of schemas', async ({ page }) => {
  await openBet(page)
  const control = page.getByRole('button', { name: 'Overview — all schemas' })
  await expect(control).toHaveAttribute('title', 'Overview — all schemas')
  await expect(page.getByRole('group', { name: 'Mode' }).getByRole('button')).toHaveText([
    'Neighbourhood',
    'Path to…',
  ])
  await control.focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/mode=overview/u)
  await expect(galaxy(page).locator('[data-cluster]')).toHaveCount(2)
  await expect(galaxy(page).locator('text')).toHaveText(['public', 'promo'])
  expect(
    Number(await galaxy(page).locator('[data-schema="public"]').getAttribute('data-objects')),
  ).toBeGreaterThan(
    Number(await galaxy(page).locator('[data-schema="promo"]').getAttribute('data-objects')),
  )
  await expect(galaxy(page).locator('[data-schema-link]')).toHaveCount(1)
  await expect(page.locator('[data-edge]')).toHaveCount(0)

  await openBet(page)
  await chart(page).hover()
  await page.keyboard.down('Control')
  for (let turn = 0; turn < 4; turn += 1) await page.mouse.wheel(0, 120)
  await page.keyboard.up('Control')
  await expect(page).toHaveURL(/mode=overview/u)
  await expect(galaxy(page)).toBeVisible()
})

test('a cluster or a chosen object flies the camera there and opens the focus', async ({
  page,
}) => {
  await openBet(page)
  await page.getByRole('button', { name: 'Overview — all schemas' }).click()
  const camera = page.locator('.camera')
  await galaxy(page).locator('[data-schema="promo"]').click()
  await expect(camera).toHaveAttribute('style', /transform: scale\(4\) translate\(/u)
  const flying = await camera.evaluate((element) =>
    element.getAnimations().map((one) => (one as CSSTransition).transitionProperty),
  )
  expect(
    flying.every((property) => property === 'transform'),
    'the flight is transform alone',
  ).toBe(true)
  await expect(page).not.toHaveURL(/mode=overview/u, { timeout: 2000 })
  const focus = chart(page).locator('[data-node][data-tier="focus"]')
  await expect(focus).toHaveAttribute('data-id', /^[a-z]+:"promo"\./u)

  await page.getByRole('button', { name: 'Overview — all schemas' }).click()
  await expect(galaxy(page)).toBeVisible()
  await choose(page, '#lineage-focus', 'public.member')
  await expect(camera).toHaveAttribute('style', /scale\(4\)/u)
  await expect(star(page, nodes.member.id)).toHaveAttribute('data-tier', 'focus', { timeout: 2000 })
})

test('a routine shows its footprint and opens its definition', async ({ page }) => {
  await openBet(page)
  await star(page, nodes.settleBet.id).click({ force: true })
  await expect.poll(() => addressed(page)).toEqual(['public', 'settle_bet', 'routine', null])
  const panel = page.getByTestId('routine-panel')
  await expect(panel).toContainText('public.settle_bet(bigint, numeric)')
  await expect(panel.locator('[data-badge="search_path_assumed"]')).toHaveText(
    'search path assumed',
  )

  const writes = panel.locator('.touch[data-access="write"]')
  await expect(writes).toHaveCount(2)
  await expect(writes.first()).toContainText('public.bet')
  await expect(writes.first()).toContainText('status')
  await expect(writes.nth(1).locator('.restricted')).toHaveText('amount (restricted)')
  await expect(panel.locator('.touch[data-access="read"]')).toContainText('stake')
  await expect(panel.getByRole('region', { name: 'Statements' })).toContainText(
    "UPDATE bet SET status = 'settled'",
  )
  await expect(panel.getByRole('region', { name: 'Calls' })).toContainText('log_event(text)')
  await expect(panel.getByRole('region', { name: 'Fires' })).toContainText(
    'bet_status_audit on bet',
  )

  await page.goto(at(nodes.purgeTable))
  const sites = page.getByTestId('routine-panel').getByRole('region', { name: 'Built at run time' })
  await expect(sites).toContainText("EXECUTE · EXECUTE format('DELETE FROM %I', t)")
  await expect(sites.locator('.confidence')).toHaveText('DYNAMIC')

  const seen = record(page)
  await page.goto(at(nodes.settleBet))
  await page.getByRole('link', { name: 'Open definition' }).click()
  await expect(page).toHaveURL(
    /\/objects\/function\?database=aviorfixture&schema=public&name=settle_bet$/u,
  )
  await expect(page.locator('h1.name')).toHaveText('public.settle_bet')
  await expect(page.locator('#database-function')).toHaveValue('aviorfixture')
  await expect.poll(() => calls(seen, 'GET', '/objects/definition').length).toBe(1)
  const read = calls(seen, 'GET', '/objects/definition')[0]?.query
  expect(['type', 'schema', 'name'].map((key) => read?.get(key))).toEqual([
    'function',
    'public',
    'settle_bet',
  ])
})

test('a building database is asked again and never drawn empty', async ({ page }) => {
  const seen = record(page)
  let asked = 0
  await answer(page, '/lineage/graph', async (route) => {
    asked += 1
    if (asked < 3) {
      await route.fulfill({ json: { state: 'building' } })
      return
    }
    const url = new URL(route.request().url())
    const reply = lineageGet('/lineage/graph', url.searchParams)
    await route.fulfill({ status: reply?.status ?? 500, json: reply?.body })
  })
  await page.goto(at(nodes.bet))
  await expect(page.getByTestId('lineage-building')).toContainText('still being built')
  await expect(chart(page)).toHaveCount(0)
  await expect(page.getByTestId('lineage-page')).not.toContainText('No relationships')
  await expect(star(page, nodes.bet.id)).toHaveAttribute('data-tier', 'focus', { timeout: 10_000 })
  await expect(page.getByTestId('lineage-building')).toHaveCount(0)
  expect(calls(seen, 'GET', '/lineage/graph')).toHaveLength(3)
})

type Running = { name: string; target: string; properties: string[]; infinite: boolean }

function running(page: Page): Promise<Running[]> {
  return page.evaluate(() =>
    document
      .getAnimations()
      .filter((animation) => animation.playState === 'running' && animation instanceof CSSAnimation)
      .flatMap((animation) => {
        const effect = animation.effect as KeyframeEffect | null
        const target = effect?.target ?? null
        if (effect === null || target === null) return []
        if (target.closest('[data-testid="lineage-page"]') === null) return []
        const properties = effect
          .getKeyframes()
          .flatMap((frame) => Object.keys(frame))
          .filter((key) => !['offset', 'easing', 'composite', 'computedOffset'].includes(key))
        return [
          {
            name: (animation as CSSAnimation).animationName.replace(/-[0-9a-f]{8}$/u, ''),
            target: target.nodeName,
            properties: [...new Set(properties)],
            infinite: effect.getComputedTiming().iterations === Infinity,
          },
        ]
      }),
  )
}

test('lineage motion uses the tokens and stops under reduced motion', async ({ page, browser }) => {
  await openBet(page, { hops: '2' })
  const entering = await running(page)
  expect(entering.map((one) => one.name)).toEqual(expect.arrayContaining(['chart-in', 'fade']))
  for (const one of entering) {
    for (const property of one.properties) expect(['opacity', 'transform']).toContain(property)
  }
  await page.waitForTimeout(1200)
  const idle = await running(page)
  expect(idle.filter((one) => one.name === 'target-ring')).toHaveLength(1)
  expect(idle.filter((one) => one.name !== 'target-ring')).toEqual([])

  await hoverEdge(page, openBetCountRead)
  await expect(page.locator('[data-particle]')).toHaveCount(6)
  const moved = await page
    .locator('[data-particle]')
    .evaluateAll((all) => all.map((one) => getComputedStyle(one).transform))
  expect(
    moved.filter((transform) => transform === 'none'),
    'every particle moves',
  ).toEqual([])
  const flowing = await running(page)
  const particles = flowing.filter((one) => one.name === 'flow-particle')
  expect(particles).toHaveLength(6)
  for (const one of particles) expect(one).toMatchObject({ target: 'SPAN', infinite: true })
  expect(flowing.filter((one) => one.target !== 'SPAN')).toEqual([])
  await page.mouse.move(5, 995)
  await expect(page.locator('[data-particle]')).toHaveCount(0)

  const calm = await browser.newPage({ reducedMotion: 'reduce' })
  await serveLineage(calm)
  await calm.goto(at(nodes.bet, { hops: '2' }))
  await expect(star(calm, nodes.bet.id)).toHaveAttribute('data-tier', 'focus')
  await calm.waitForTimeout(300)
  await hoverEdge(calm, openBetCountRead)
  expect((await running(calm)).filter((one) => one.name !== 'fade')).toEqual([])
  await calm.getByRole('button', { name: 'Path to…' }).click()
  await choose(calm, '#lineage-to', 'promo.campaign')
  await expect(calm.getByTestId('path-panel').locator('button.route').first()).toBeVisible()
  expect((await running(calm)).filter((one) => one.name !== 'fade')).toEqual([])
  await calm.close()
})

async function follow(page: Page, url: string): Promise<void> {
  await page.evaluate((next) => {
    window.history.pushState({}, '', next)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, url)
}

test('a name the server cannot settle is refused in its words, and a seen overload is offered', async ({
  page,
}) => {
  await openBet(page)
  await star(page, nodes.openBetCount.id).click({ force: true })
  await expect
    .poll(() => addressed(page))
    .toEqual(['public', 'open_bet_count', 'routine', 'bigint'])
  await expect(star(page, nodes.openBetCount.id)).toHaveAttribute('data-tier', 'focus')

  await follow(page, at(nodes.openBetCount))
  await expect(page.getByRole('alert')).toHaveText(
    'More than one routine is called public.open_bet_count. Give its signature to choose one.',
  )
  const offered = page.getByRole('group', { name: 'Overloads' }).locator('button.overload')
  await expect(offered).toHaveText([
    'public.open_bet_count(bigint)',
    'public.open_bet_count(bigint, date)',
  ])
  await offered.nth(1).click()
  await expect
    .poll(() => addressed(page))
    .toEqual(['public', 'open_bet_count', 'routine', 'bigint, date'])
  await expect(star(page, nodes.openBetCountByDay.id)).toHaveAttribute('data-tier', 'focus')
  await expect(page.getByRole('group', { name: 'Overloads' })).toHaveCount(0)

  await page.goto('/lineage?database=aviorfixture&schema=public&name=ghost&kind=table')
  await expect(page.getByRole('alert')).toHaveText('public.ghost is not in this database.')
  await expect(chart(page)).toHaveCount(0)
})

type Rect = { left: number; top: number; right: number; bottom: number }

const overlap = (a: Rect, b: Rect) =>
  Math.min(a.right, b.right) - Math.max(a.left, b.left) > 0.5 &&
  Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0.5

const rectOf = (locator: Locator): Promise<Rect> =>
  locator.evaluate((element) => {
    const { left, top, right, bottom } = element.getBoundingClientRect()
    return { left, top, right, bottom }
  })

async function serveRound(page: Page): Promise<string> {
  await page.route('**/api/v1/lineage/graph**', (route) => route.fulfill({ json: roundTwoHops }))
  const query = { database: 'aviorlineage', schema: 'public', name: 'round', kind: 'table' }
  return `/lineage?${new URLSearchParams({ ...query, hops: '2' }).toString()}`
}

/** Drawn and still: labels glide to a new place when the web font arrives and they are measured again. */
async function drawn(page: Page, address: string): Promise<void> {
  await page.goto(address)
  await expect(chart(page).locator('[data-node][data-tier="focus"]')).toHaveCount(1)
  await page.evaluate(() => document.fonts.ready)
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document
            .getAnimations()
            .filter(
              (one) =>
                one.playState === 'running' && one.effect?.getTiming().iterations !== Infinity,
            ).length,
      ),
    )
    .toBe(0)
}

async function labelsAreClear(page: Page, where: string): Promise<void> {
  const outer = await rectOf(chart(page))
  const labels = chart(page).locator('text.label')
  const boxes = await labels.evaluateAll((texts) =>
    texts.map((text) => {
      const { left, top, right, bottom } = text.getBoundingClientRect()
      return { id: text.getAttribute('data-label-for') ?? '', left, top, right, bottom }
    }),
  )
  expect(boxes.length, `${where}: labels drawn`).toBeGreaterThan(0)
  for (const box of boxes) {
    const inside =
      box.left >= outer.left - 0.5 &&
      box.right <= outer.right + 0.5 &&
      box.top >= outer.top - 0.5 &&
      box.bottom <= outer.bottom + 0.5
    expect(inside, `${where}: ${box.id} is inside the chart`).toBe(true)
  }
  for (const [index, a] of boxes.entries())
    for (const b of boxes.slice(index + 1))
      expect(overlap(a, b), `${where}: ${a.id} and ${b.id} do not cross`).toBe(false)
  const untitled = await labels.evaluateAll((texts) =>
    texts
      .filter((text) => text.querySelector('tspan')?.textContent?.endsWith('…') === true)
      .filter((text) => text.querySelector('title') === null)
      .map((text) => text.getAttribute('data-label-for')),
  )
  expect(untitled, `${where}: a cut name keeps the whole one in its title`).toEqual([])
}

test('labels stay inside the chart and never cross, at two hops and at the cap', async ({
  page,
}) => {
  await drawn(page, await serveRound(page))
  await labelsAreClear(page, 'two hops')
  const cut = chart(page).locator(
    `text.label${is('data-label-for', 'function:"public"."archive_table"(p_table text)')}`,
  )
  await expect(cut.locator('tspan').first()).toHaveText('archive_table')
  await expect(cut.locator('.star-badge')).toHaveText('DYNAMIC')
  await drawn(page, await serveCapLineage(page))
  await labelsAreClear(page, 'the cap')
})

test('the focus label is clear of its star, its ring and the edges', async ({ page }) => {
  await drawn(page, await serveRound(page))
  const focus = roundTwoHops.focus ?? ''
  const label = chart(page).locator(`text.label${is('data-label-for', focus)}`)
  await expect(label.locator('tspan').first()).toHaveText('round')
  const box = await rectOf(label)
  expect(overlap(box, await rectOf(star(page, focus))), 'clear of the star').toBe(false)
  const ring = await page.locator('.target-ring').evaluate((element) => {
    // At rest, as the operator sees it; the pulse grows it as it fades out.
    ;(element as HTMLElement).style.animation = 'none'
    const { left, top, right, bottom } = element.getBoundingClientRect()
    return { left, top, right, bottom }
  })
  expect(overlap(box, ring), 'clear of the ring').toBe(false)
  const drawnOver = await label.evaluate((text) => {
    const style = getComputedStyle(text)
    const edges = text.closest('svg')?.querySelector('g.edges')
    const after = edges ? edges.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING : 0
    return {
      halo: style.paintOrder.startsWith('stroke') && style.strokeWidth !== '0px',
      after: after !== 0,
    }
  })
  expect(drawnOver, 'drawn over the edges on a halo of the ground colour').toEqual({
    halo: true,
    after: true,
  })
})

test('the dynamic tray never covers the legend at 1440×900 or 1280×800', async ({ page }) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1280, height: 800 },
  ]) {
    await page.setViewportSize(viewport)
    await drawn(page, await serveRound(page))
    const legend = page.getByRole('list', { name: 'Legend' })
    const chip = page.getByTestId('dynamic-chip')
    await expect(chip).toBeVisible()
    expect(overlap(await rectOf(chip), await rectOf(legend)), 'the chip beside it').toBe(false)
    await chip.click()
    const tray = page.getByTestId('dynamic-tray')
    await expect(tray).toBeVisible()
    await expect(legend).toBeInViewport({ ratio: 1 })
    const where = `${viewport.width}×${viewport.height}`
    expect(overlap(await rectOf(tray), await rectOf(legend)), where).toBe(false)
  }
})
