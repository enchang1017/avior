import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { serveObservatory } from './visual/observatory'

/*
 * FEAT-071's page against the fixed API. The e2e stack carries no snapshot
 * history, so the observatory's answers come from fixtures covering every
 * sentence the page can write, and the page is read the way an operator
 * reads it.
 */

test.use({ viewport: { width: 1440, height: 1000 }, timezoneId: 'UTC', locale: 'en-US' })

const banned = /\b(dead|unused|obsolete|deprecated)\b/iu

async function alignedWith(page: Page, checkbox: string, select: string): Promise<void> {
  const box = await page.locator(checkbox).locator('xpath=ancestor::label[1]').boundingBox()
  const tick = await page.locator(checkbox).boundingBox()
  const control = await page.locator(select).boundingBox()
  if (box === null || tick === null || control === null) throw new Error('a control has no box')
  const centre = (b: { y: number; height: number }) => b.y + b.height / 2
  expect(
    Math.abs(centre(box) - centre(control)),
    'the checkbox row centres on the select',
  ).toBeLessThanOrEqual(2)
  expect(
    Math.abs(centre(tick) - centre(control)),
    'the checkbox itself centres on the select',
  ).toBeLessThanOrEqual(2)
  expect(
    Math.abs(box.height - control.height),
    'the checkbox row is a control tall',
  ).toBeLessThanOrEqual(2)
}

async function open(page: Page, caller: string, tab = '', statementsInstalled = true) {
  const missed = await serveFixedApi(page)
  await serveObservatory(page, caller, statementsInstalled)
  await page.goto(tab === '' ? '/observatory' : `/observatory?tab=${tab}`)
  await expect(page.getByRole('heading', { name: 'Observatory' })).toBeVisible()
  return missed
}

const tabs = (page: Page) =>
  page.getByRole('tablist', { name: 'Observatory views' }).getByRole('tab')

// FEAT-071: the owner opened the observatory to every signed-in operator on 2026-09-27.
test('the observatory opens on Overview with every tab for every caller', async ({ page }) => {
  for (const caller of ['administrator', 'dba', 'developer']) {
    const missed = await open(page, caller)
    await expect(tabs(page)).toHaveText([
      'Overview',
      'Avior usage',
      'Capacity',
      'Last used',
      'Performance',
      'Routine timing',
    ])
    await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('link', { name: 'Observatory' })).toBeVisible()
    expect(missed).toEqual([])
    await page.unrouteAll({ behavior: 'ignoreErrors' })
  }
})

test('a developer reads everybody’s usage and the server tabs', async ({ page }) => {
  await open(page, 'developer', 'usage')
  await expect(page.getByTestId('usage-table')).toContainText('lin.wei')
  await expect(page.getByText('Every operator')).toBeVisible()
  await page.goto('/observatory?tab=last-used')
  await expect(page.getByTestId('objects-table')).toBeVisible()
})

test('usage links each row to the audit log filtered to its operator and database', async ({
  page,
}) => {
  await open(page, 'administrator', 'usage')
  const link = page.getByTestId('usage-table').getByRole('link', { name: 'Audit log' })
  const target = new URL((await link.getAttribute('href')) ?? '', 'http://x')
  expect(target.pathname).toBe('/audit/all')
  expect(target.searchParams.get('operator')).toBe('lin.wei')
  expect(target.searchParams.get('database')).toBe('aviorfixture')
  expect(target.searchParams.get('from')).not.toBeNull()
})

test('capacity draws each database with its growth line and flagged count', async ({ page }) => {
  await open(page, 'dba', 'capacity')
  const table = page.getByTestId('capacity-table')
  await expect(table.getByRole('row')).toHaveCount(3)
  await expect(
    table.getByRole('img', { name: 'aviorfixture size over 30 days: rising' }),
  ).toBeVisible()
  await expect(table).toContainText('3 flagged')
})

test('FEAT-071 AC-22: the Last used tab never calls an object dead, unused, obsolete or deprecated', async ({
  page,
}) => {
  await open(page, 'dba', 'last-used')
  await expect(page.getByTestId('objects-table')).toBeVisible()
  const text = await page.locator('body').innerText()
  expect(text).toContain('last written 2026-08-02 (49 days ago)')
  expect(text).toContain('not seen since tracking began on 2026-07-01')
  expect(text).toContain('not tracked (LANGUAGE sql needs track_functions = all)')
  const flag = page.locator('[data-flag]').first()
  await expect(flag).toContainText('idle > 30 days')
  await expect(flag).toHaveAttribute('title', 'flagged: last use more than 30 days ago')
  await expect(flag).toHaveAccessibleName(/flagged: last use more than 30 days ago/u)
  const box = await flag.boundingBox()
  expect(box?.height ?? 0).toBeLessThan(24)
  expect(text.match(banned)).toBeNull()
})

test('FEAT-071 AC-23: the Last used tab lists no columns and says why', async ({ page }) => {
  await open(page, 'administrator', 'last-used')
  await expect(page.getByTestId('columns-note')).toHaveText(
    'Columns are not listed: PostgreSQL keeps no per-column usage statistics.',
  )
  const kinds = await page
    .getByTestId('objects-table')
    .locator('tbody tr:not([data-routine-group]) td:nth-child(2)')
    .allInnerTexts()
  expect(new Set(kinds.map((kind) => kind.trim()))).toEqual(
    new Set(['table', 'index', 'function', 'procedure']),
  )
})

test('date-versioned routines sit in the objects table under their group, newest first', async ({
  page,
}) => {
  await open(page, 'dba', 'last-used')
  const table = page.getByTestId('objects-table')
  const header = table.locator('[data-routine-group]')
  await expect(header).toHaveCount(1)
  await expect(header).toContainText('public.shop_settle_order')
  await expect(header).toContainText('3 versions · 1 flagged')
  const versions = table.locator('[data-routine-version]')
  await expect(versions).toHaveCount(3)
  await expect(versions.nth(0)).toContainText('shop_settle_order_26_09_25')
  await expect(versions.nth(1)).toContainText('shop_settle_order_26_09_21')
  await expect(versions.nth(2)).toContainText('shop_settle_order_26_09_19')
  await expect(versions.nth(0).getByText('latest', { exact: true })).toBeVisible()
  await expect(table.getByText('latest', { exact: true })).toHaveCount(1)
  await expect(header.locator('xpath=following-sibling::tr[1]')).toContainText(
    'shop_settle_order_26_09_25',
  )
})

test('flagged only keeps the flagged objects and the groups that hold one', async ({ page }) => {
  await open(page, 'dba', 'last-used')
  await page.locator('#last-used-flagged').click()
  await page.getByRole('option', { name: 'Flagged only', exact: true }).click()
  const rows = page.getByTestId('objects-table').locator('tbody tr')
  await expect(rows).toHaveCount(4)
  await expect(rows.first()).toHaveAttribute('data-flagged', 'true')
  await expect(page.locator('[data-routine-group]')).toContainText('3 versions · 1 flagged')
  const versions = page.locator('[data-routine-version]')
  await expect(versions).toHaveCount(1)
  await expect(versions).toContainText('shop_settle_order_26_09_19')
  await expect(versions).not.toContainText('latest')
})

test('performance lists the costly statements, what is running, and table health', async ({
  page,
}) => {
  await open(page, 'dba', 'performance')
  const statement = page.getByTestId('top-statements').locator('[data-statement-text]')
  await expect(statement).toHaveClass(/line-clamp-2/u)
  await page.getByTestId('top-statements').getByRole('button', { name: 'More' }).click()
  await expect(statement).not.toHaveClass(/line-clamp-2/u)
  await expect(page.getByTestId('running-now')).toContainText('4242')
  await expect(page.getByTestId('table-health')).toContainText('10.0%')
  await expect(page.getByTestId('top-statements')).toContainText('Cumulative since 09-01 00:00.')
})

test("performance leaves Avior's own monitoring out until it is asked for", async ({ page }) => {
  await open(page, 'dba')
  const requests: URL[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/observatory/statements')) requests.push(url)
  })
  await page.goto('/observatory?tab=performance')
  const toggle = page.getByLabel("Include Avior's own monitoring")
  await expect(toggle).toBeVisible()
  await expect(toggle).not.toBeChecked()
  await expect.poll(() => requests.length).toBeGreaterThan(0)
  expect(requests.every((url) => url.searchParams.get('includeOwn') !== 'true')).toBe(true)
  const asked = page.waitForRequest(
    (request) =>
      request.url().includes('/observatory/statements') &&
      new URL(request.url()).searchParams.get('includeOwn') === 'true',
  )
  await toggle.check()
  await asked
})

test('performance shows the reason when pg_stat_statements is missing', async ({ page }) => {
  await open(page, 'administrator', 'performance', false)
  await expect(page.getByTestId('statements-failure')).toContainText(
    'pg_stat_statements is not installed in aviordb',
  )
})

test('FEAT-071 AC-29b: the include-own checkbox sits on the centre line of the selects beside it', async ({
  page,
}) => {
  await open(page, 'dba', 'performance')
  await expect(page.getByTestId('include-own')).toBeVisible()
  await alignedWith(page, '[data-testid="include-own"]', '#statements-database')
})
