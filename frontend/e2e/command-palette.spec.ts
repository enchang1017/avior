import { expect, test, type Page, type Route } from '@playwright/test'

import type { components } from '@/gen/api'
import { serveFixedApi } from './visual/api'
import { answer, answerJson, calls, record } from './redesign/fixtures'

type SearchResult = components['schemas']['SearchResult']

// Past the palette's 120 ms debounce and short of its first 1 s retry, so a
// step of the page's clock settles one search and starts no second one.
const settleMs = 500

const palette = (page: Page) => page.getByTestId('palette')
const input = (page: Page) => palette(page).getByRole('combobox', { name: 'Find' })
const trigger = (page: Page) =>
  page.getByRole('button', { name: 'Find a table, request, operator' })

const tableHits: SearchResult = {
  hits: [
    {
      kind: 'table',
      label: 'public.member',
      database: 'aviorfixture',
      schema: 'public',
      name: 'member',
      objectKind: 'table',
    },
    {
      kind: 'table',
      label: 'public.member_summary',
      database: 'aviorfixture',
      schema: 'public',
      name: 'member_summary',
      objectKind: 'view',
    },
  ],
  limit: 20,
  indexing: 0,
}

async function open(page: Page): Promise<void> {
  await expect(trigger(page)).toBeVisible()
  await page.keyboard.press('ControlOrMeta+k')
  await expect(input(page)).toBeFocused()
}

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
  await answerJson(page, '/search', tableHits)
})

test('the palette opens on every page and gives focus back on escape', async ({ page }) => {
  for (const path of ['/', '/audit', '/writes', '/releases', '/admin/accounts']) {
    await page.goto(path)
    await trigger(page).focus()
    await open(page)
    await page.keyboard.press('Escape')
    await expect(palette(page)).toBeHidden()
    await expect(trigger(page)).toBeFocused()
  }

  await open(page)
  await page.keyboard.type('pending')
  await expect(palette(page).getByRole('option')).toHaveCount(3)
  await expect(palette(page).getByRole('option').first()).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowDown')
  await expect(palette(page).getByRole('option').nth(1)).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await expect(palette(page)).toBeHidden()
  await expect(page).toHaveURL(/\/releases$/u)
})

test('pages match from the first letter and only reachable ones are listed', async ({ page }) => {
  await page.route('**/api/v1/auth/me', (route) =>
    route.fulfill({
      json: {
        id: 'op-dev',
        username: 'developer',
        roles: ['developer'],
        permissions: ['query.inspect', 'audit.read.own'],
        features: ['write', 'release'],
        environment: 'staging',
        selfApproval: false,
        instance: { id: 'local', name: 'Local PostgreSQL' },
      },
    }),
  )
  const seen = record(page)
  await page.goto('/')
  await open(page)
  await page.keyboard.type('a')
  const options = palette(page).getByRole('option')
  await expect(options.first()).toBeVisible()
  const labels = await options.allTextContents()
  expect(labels.some((label) => label.includes('Personal Log'))).toBe(true)
  expect(labels.some((label) => label.includes('Account Management'))).toBe(false)
  expect(labels.some((label) => label.includes('Log') && label.startsWith('Admin'))).toBe(false)
  await page.waitForTimeout(300)
  expect(calls(seen, 'GET', '/search')).toHaveLength(0)
})

test('choosing a table points the console at it', async ({ page }) => {
  await page.goto('/audit')
  await open(page)
  await page.keyboard.type('memb')
  await expect(
    palette(page)
      .getByRole('option', { name: /public\.member\b/u })
      .first(),
  ).toBeVisible()
  await palette(page)
    .getByRole('option', { name: /public\.member\s/u })
    .click()
  await expect(page).toHaveURL(/database=aviorfixture/u)
  await expect(page.locator('button.node.current')).toContainText('member')
  await expect(page.locator('.cm-content')).toContainText(
    'SELECT id, username FROM public.member LIMIT 100',
  )
})

const memberRow = (page: Page) => palette(page).getByRole('option', { name: /public\.member\s/u })

async function findMember(page: Page): Promise<void> {
  await open(page)
  await page.keyboard.type('memb')
  await expect(memberRow(page)).toBeVisible()
}

test('a table row offers query, object and lineage, by keyboard and by click', async ({ page }) => {
  await page.goto('/audit')
  await findMember(page)
  await expect(memberRow(page).locator('button.action')).toHaveText(['Query', 'Object', 'Lineage'])
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('ArrowRight')
  await expect(memberRow(page).locator('button.action[data-active="true"]')).toHaveText('Lineage')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/lineage\?.*name=member/u)
  await expect(page).toHaveURL(/kind=table/u)

  await page.goto('/audit')
  await findMember(page)
  await memberRow(page)
    .getByRole('button', { name: 'Open public.member in its object viewer' })
    .click()
  await expect(page).toHaveURL(/\/objects\/table\?database=aviorfixture&schema=public&name=member/u)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('public.member')

  await page.goto('/audit')
  await findMember(page)
  await page.keyboard.press('Tab')
  await page.keyboard.press('Shift+Tab')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/\?database=aviorfixture.*table=member/u)
})

test('an action whose page the caller may not open is left out', async ({ page }) => {
  await page.route('**/api/v1/auth/me', (route) =>
    route.fulfill({
      json: {
        id: 'op-approver',
        username: 'approver',
        roles: ['release_approver'],
        permissions: ['release.approve'],
        features: ['release'],
        environment: 'staging',
        selfApproval: false,
        instance: { id: 'local', name: 'Local PostgreSQL' },
      },
    }),
  )
  await page.goto('/releases')
  await findMember(page)
  await expect(memberRow(page).locator('button.action')).toHaveCount(0)
})

test('row actions are named buttons announced as the active option', async ({ page }) => {
  await page.goto('/audit')
  await findMember(page)
  const buttons = memberRow(page).getByRole('button')
  await expect(buttons).toHaveCount(3)
  const ids = await buttons.evaluateAll((all) => all.map((one) => one.id))
  await expect(input(page)).toHaveAttribute('aria-activedescendant', ids[0] ?? '')
  await expect(
    palette(page).getByRole('button', { name: 'Open public.member in the query console' }),
  ).toHaveAttribute('id', ids[0] ?? '')
  await page.keyboard.press('ArrowRight')
  await expect(input(page)).toHaveAttribute('aria-activedescendant', ids[1] ?? '')
  await expect(
    palette(page).getByRole('button', { name: 'Open public.member in Lineage' }),
  ).toHaveAttribute('id', ids[2] ?? '')
  await expect(palette(page).locator('footer')).toContainText('destination')
})

test('the same query behaves the same on every page', async ({ page }) => {
  for (const path of ['/', '/lineage', '/lineage?database=aviorfixture&mode=overview', '/audit']) {
    await page.goto(path)
    await findMember(page)
    await expect(memberRow(page).locator('button.action')).toHaveText([
      'Query',
      'Object',
      'Lineage',
    ])
    await page.keyboard.press('Enter')
    await expect(page, `from ${path}`).toHaveURL(/\/\?database=aviorfixture.*table=member/u)
  }
})

test('typing debounces, cancels, and never paints a stale answer', async ({ page }) => {
  const held: { q: string; route: Route }[] = []
  await answer(page, '/search', (route) => {
    const q = new URL(route.request().url()).searchParams.get('q') ?? ''
    held.push({ q, route })
  })
  const seen = record(page)
  await page.clock.install()
  await page.goto('/')
  await open(page)

  /*
   * The page's clock stands still while keys go in, so the debounce is measured
   * against the page's time rather than against how fast a loaded machine
   * delivers keystrokes. Typed on the wall clock, a gap over 120 ms between two
   * keys sent `q=me` first, and the case failed for a reason that was never the
   * palette's.
   */
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  await page.keyboard.type('m')
  await page.clock.runFor(300)
  expect(calls(seen, 'GET', '/search')).toHaveLength(0)

  await page.keyboard.type('emb')
  await page.clock.runFor(settleMs)
  await expect.poll(() => calls(seen, 'GET', '/search').length).toBe(1)
  expect(calls(seen, 'GET', '/search')[0]?.query.get('q')).toBe('memb')

  const failed: string[] = []
  page.on('requestfailed', (request) =>
    failed.push(new URL(request.url()).searchParams.get('q') ?? ''),
  )
  await page.keyboard.type('e')
  await page.clock.runFor(settleMs)
  await expect.poll(() => held.length).toBe(2)
  await expect.poll(() => failed).toContain('memb')

  const [older, newer] = held
  await newer?.route.fulfill({
    json: {
      hits: [{ kind: 'database', label: 'membership', database: 'membership' }],
      limit: 20,
      indexing: 1,
    },
  })
  await expect(palette(page).getByRole('option', { name: /membership/u })).toBeVisible()
  await older?.route.fulfill({ json: tableHits }).catch(() => undefined)
  await page.waitForTimeout(200)
  await expect(palette(page).getByRole('option', { name: /public\.member/u })).toHaveCount(0)
  await expect(palette(page).getByText('still indexing 1 database')).toBeVisible()
})

test('a query typed before search has loaded is answered once it has', async ({ page }) => {
  let release: () => void = () => undefined
  const held = new Promise<void>((resolve) => (release = resolve))
  await page.route(
    /\/(register-[\w-]+\.js|features\/search\/register\.ts)(\?.*)?$/u,
    async (route) => {
      await held
      await route.continue()
    },
  )
  await page.goto('/')
  await open(page)
  await page.keyboard.type('qu')
  await page.waitForTimeout(300)
  await expect(palette(page).getByRole('option')).toHaveCount(0)

  release()
  await expect(palette(page).getByRole('option', { name: 'Query page', exact: true })).toBeVisible()
})

test('a search answered while indexing is asked again until it is complete', async ({ page }) => {
  const asked: string[] = []
  await answer(page, '/search', async (route) => {
    asked.push(new URL(route.request().url()).searchParams.get('q') ?? '')
    const body =
      asked.length === 1 ? { hits: [], limit: 20, indexing: 2 } : { ...tableHits, indexing: 0 }
    await route.fulfill({ json: body })
  })
  await page.goto('/audit')
  await open(page)
  await page.keyboard.type('me')
  await expect(palette(page).locator('.status')).toHaveText('still indexing 2 databases')
  await expect(palette(page).getByRole('option', { name: /public\.member\s/u })).toBeVisible()
  await expect(palette(page).locator('.status')).toHaveCount(0)
  expect(asked).toEqual(['me', 'me'])
  await page.waitForTimeout(1500)
  expect(asked).toEqual(['me', 'me'])
})
