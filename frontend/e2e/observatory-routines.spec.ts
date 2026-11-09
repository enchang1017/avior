import { expect, test, type Page, type Request } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { observatoryRoutines, serveObservatory } from './visual/observatory'

/*
 * FEAT-073's page against the fixed API: the Routine timing tab read the way
 * an operator reads it, its choices kept in the address, and the overview's
 * chart that opens it.
 */

test.use({ viewport: { width: 1440, height: 900 }, timezoneId: 'UTC', locale: 'en-US' })

async function open(page: Page, query: string) {
  const missed = await serveFixedApi(page)
  await serveObservatory(page, 'developer')
  await page.goto(`/observatory${query}`)
  await expect(page.getByRole('heading', { name: 'Observatory' })).toBeVisible()
  return missed
}

const table = (page: Page) => page.getByTestId('routines-table')
const row = (page: Page, name: string) =>
  table(page).locator('tr[data-routine-row]', { hasText: name })

function routineAsks(page: Page): URLSearchParams[] {
  const asked: URLSearchParams[] = []
  page.on('request', (request: Request) => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/observatory/routines')) asked.push(url.searchParams)
  })
  return asked
}

const lastAsk = (asked: URLSearchParams[]) =>
  Object.fromEntries(asked.at(-1)?.entries() ?? ([] as [string, string][]))

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

async function choose(page: Page, select: string, option: string) {
  await page.locator(select).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('FEAT-073 AC-10: the tab lists each routine with its timing, the labelled slowest call and the caption', async ({
  page,
}) => {
  const missed = await open(page, '?tab=routines&uncalled=shown')
  await expect(page.getByRole('tab', { name: 'Routine timing' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(table(page).locator('thead th')).toHaveText([
    'Database',
    'Routine',
    'Calls',
    /^Mean per call/u,
    'Total time',
    'Self time',
    'Slowest top-level call',
  ])

  const caption = page.getByTestId('routines-caption')
  await expect(caption).toContainText(
    'Averages over 15-minute snapshots: one slow call among many fast ones can hide.',
  )
  await expect(caption).toContainText(/Tracked since (2026-)?09-01 00:00\./u)
  await expect(caption).toContainText('LANGUAGE sql routines are not timed.')

  const settle = row(page, 'public.settle_round')
  await expect(settle.locator('td')).toHaveText([
    'aviorfixture',
    'public.settle_round',
    '48,000',
    '19 ms',
    '15 min 12 s',
    '10 min 4 s',
    /2\.4 s\s*top-level calls since (2026-)?09-03 00:00/u,
  ])

  const close = row(page, 'ops.close_day')
  await expect(close).toContainText('procedure')
  await expect(close.locator('[data-cell="max"]')).toHaveText('')

  const uncalled = row(page, 'public.lookup_member')
  await expect(uncalled.locator('td').nth(2)).toHaveText('0')
  await expect(uncalled.locator('[data-cell="mean"]')).toHaveText('—')
  await expect(row(page, 'public.member_label').locator('[data-cell="mean"]')).toHaveText(
    'not timed (LANGUAGE sql needs track_functions = all)',
  )

  const group = table(page).locator('tr[data-routine-group]')
  await expect(group).toHaveCount(1)
  await expect(group).toContainText('public.shop_settle_order')
  await expect(group).toContainText('3 versions')
  const versions = table(page).locator('tr[data-routine-version]')
  await expect(versions.locator('td:nth-child(2)')).toHaveText([
    /shop_settle_order_26_09_25\s*latest/u,
    'public.shop_settle_order_26_09_01',
    'public.shop_settle_order_26_08_14',
  ])
  await expect(table(page)).not.toContainText('(bigint)')
  expect(missed).toEqual([])
})

test('FEAT-073 AC-10a: a routine is its name alone, linked to its definition, the signature as the title', async ({
  page,
}) => {
  await open(page, '?tab=routines')
  const settle = row(page, 'public.settle_round').locator('[data-routine-link]')
  await expect(settle).toHaveText('public.settle_round')
  await expect(settle).toHaveAttribute('title', 'public.settle_round(bigint)')
  await expect(settle).toHaveAttribute(
    'href',
    '/objects/function?database=aviorfixture&schema=public&name=settle_round',
  )
  await expect(row(page, 'ops.close_day').locator('[data-routine-link]')).toHaveAttribute(
    'href',
    /^\/objects\/stored-procedure\?database=[^&]+&schema=ops&name=close_day$/u,
  )
  await settle.click()
  await expect(page).toHaveURL(
    /\/objects\/function\?database=aviorfixture&schema=public&name=settle_round$/u,
  )
})

test('FEAT-073 AC-10b: with no slowest call on the page the column is dropped and the caption says why', async ({
  page,
}) => {
  await serveFixedApi(page)
  await serveObservatory(page, 'developer')
  await page.route('**/api/v1/observatory/routines**', (route) =>
    route.fulfill({
      json: {
        ...observatoryRoutines,
        routines: observatoryRoutines.routines.map((one) => ({ ...one, maxCallMs: null })),
      },
    }),
  )
  await page.goto('/observatory?tab=routines')
  await expect(table(page)).toBeVisible()
  await expect(table(page).locator('thead th')).toHaveText([
    'Database',
    'Routine',
    'Calls',
    /^Mean per call/u,
    'Total time',
    'Self time',
  ])
  await expect(table(page).locator('[data-cell="max"]')).toHaveCount(0)
  await expect(page.getByTestId('routines-no-slowest')).toHaveText(
    'Slowest single call is shown only for routines an application calls directly with SELECT f(…); none on this page.',
  )
})

test('FEAT-073 AC-10c: routines with no calls are hidden by default and counted, with the header of a group left empty', async ({
  page,
}) => {
  await open(page, '?tab=routines')
  const toggle = page.getByLabel('Hide routines with 0 calls')
  await expect(toggle).toBeChecked()
  await expect(row(page, 'public.lookup_member')).toHaveCount(0)
  await expect(page.getByTestId('routines-hidden')).toHaveText(
    /^\d+ routines? with no calls hidden$/u,
  )

  await toggle.uncheck()
  await expect(page).toHaveURL(/uncalled=shown/u)
  await expect(row(page, 'public.lookup_member')).toHaveCount(1)
  await expect(page.getByTestId('routines-hidden')).toHaveCount(0)
  await page.reload()
  await expect(page.getByLabel('Hide routines with 0 calls')).not.toBeChecked()
})

test('FEAT-073 AC-10d: one database chosen drops the database column, and all databases bring it back', async ({
  page,
}) => {
  await open(page, '?tab=routines&database=aviorfixture')
  await expect(table(page).locator('thead th').first()).toHaveText('Routine')
  await choose(page, '#routines-database', 'All databases')
  await expect(table(page).locator('thead th').first()).toHaveText('Database')
})

test('FEAT-073 AC-11: the window, sort, database and search re-read the list and survive a reload', async ({
  page,
}) => {
  const asked = routineAsks(page)
  await open(page, '?tab=routines')
  await expect(table(page)).toBeVisible()
  expect(lastAsk(asked)).toEqual({ range: '7d', sort: 'mean' })

  await choose(page, '#routines-range', 'Last 30 days')
  await expect(page).toHaveURL(/range=30d/u)
  await expect.poll(() => lastAsk(asked)).toEqual({ range: '30d', sort: 'mean' })

  await table(page)
    .getByRole('button', { name: /^Calls/u })
    .click()
  await expect(page).toHaveURL(/sort=calls/u)
  await expect.poll(() => lastAsk(asked)).toEqual({ range: '30d', sort: 'calls' })
  await expect(table(page).locator('tr[data-routine-row]').first()).toContainText('settle_round')

  await choose(page, '#routines-database', 'aviorfixture')
  await expect(page).toHaveURL(/database=aviorfixture/u)
  await expect
    .poll(() => lastAsk(asked))
    .toEqual({ range: '30d', sort: 'calls', database: 'aviorfixture' })
  await expect(row(page, 'ops.close_day')).toHaveCount(0)

  await page.locator('#routines-search').fill('shop_settle')
  await expect(page).toHaveURL(/search=shop_settle/u)
  await expect
    .poll(() => lastAsk(asked))
    .toEqual({ range: '30d', sort: 'calls', database: 'aviorfixture', search: 'shop_settle' })
  await expect(row(page, 'settle_round')).toHaveCount(0)
  await expect(table(page).locator('tr[data-routine-version]')).toHaveCount(3)

  asked.length = 0
  await page.reload()
  await expect(table(page)).toBeVisible()
  expect(lastAsk(asked)).toEqual({
    range: '30d',
    sort: 'calls',
    database: 'aviorfixture',
    search: 'shop_settle',
  })
  await expect(page.locator('#routines-range')).toHaveValue('Last 30 days')
  await expect(table(page).locator('th[data-sort="calls"]')).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  await expect(page.locator('#routines-database')).toHaveValue('aviorfixture')
  await expect(page.locator('#routines-search')).toHaveValue('shop_settle')
})

async function routineChart(page: Page) {
  const scroller = page.getByTestId('overview-scroll')
  await scroller.focus()
  const chart = page.locator('[data-chart="routines"]')
  await expect(async () => {
    await page.keyboard.press('End')
    await expect(chart.getByTestId('chart-bars')).toBeVisible({ timeout: 1000 })
  }).toPass()
  return chart
}

test('FEAT-073 AC-12: the overview chart opens the tab sorted by total time, and a bar opens it searched to its routine', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 600 })
  const asked = routineAsks(page)
  await open(page, '?range=30d')
  const chart = await routineChart(page)
  await expect(chart.locator('figcaption')).toContainText(
    'The ten routines with the most total time over the range',
  )
  expect(lastAsk(asked)).toEqual({ range: '30d', sort: 'total', limit: '10' })
  await expect(chart.locator('[data-testid="chart-bars"] button').first()).toContainText(
    'public.settle_round',
  )

  await chart.getByRole('link', { name: 'Open routine timing' }).click()
  await expect(page).toHaveURL(/tab=routines/u)
  await expect(page).toHaveURL(/sort=total/u)
  await expect(page).toHaveURL(/range=30d/u)
  await expect(table(page).locator('th[data-sort="total"]')).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  await expect(table(page).locator('tr[data-routine-row]').first()).toContainText('settle_round')

  await page.goto('/observatory?range=7d')
  const again = await routineChart(page)
  await again.locator('[data-testid="chart-bars"] button', { hasText: 'ops.close_day' }).click()
  await expect(page).toHaveURL(/tab=routines/u)
  const address = new URL(page.url()).searchParams
  expect(address.get('sort')).toBe('total')
  expect(address.get('range')).toBe('7d')
  expect(address.get('search')).toBe('ops.close_day')
  expect(address.get('database')).toBe('ledger')
  await expect(page.locator('#routines-search')).toHaveValue('ops.close_day')
  await expect(table(page).locator('tr[data-routine-row]')).toHaveCount(1)
  await expect(row(page, 'ops.close_day')).toBeVisible()
})

test('FEAT-073 AC-11a: the hide-0-calls checkbox sits on the centre line of the selects beside it', async ({
  page,
}) => {
  await open(page, '?tab=routines')
  await expect(table(page)).toBeVisible()
  await alignedWith(page, '[data-testid="hide-uncalled"]', '#routines-database')
})

test('FEAT-073 AC-11b: a header sorts by its column, a second click turns it round, and the address and aria-sort follow', async ({
  page,
}) => {
  const asked = routineAsks(page)
  await open(page, '?tab=routines&range=30d&uncalled=shown')
  await expect(table(page)).toBeVisible()
  const mean = table(page).locator('th[data-sort="mean"]')
  await expect(mean).toHaveAttribute('aria-sort', 'descending')
  await expect(mean).toContainText('▼')

  const total = table(page).locator('th[data-sort="total"]')
  await expect(total).toHaveAttribute('aria-sort', 'none')
  await total.getByRole('button').click()
  await expect(page).toHaveURL(/sort=total/u)
  await expect(page).not.toHaveURL(/order=/u)
  await expect(total).toHaveAttribute('aria-sort', 'descending')
  await expect(mean).toHaveAttribute('aria-sort', 'none')
  await expect.poll(() => lastAsk(asked)).toEqual({ range: '30d', sort: 'total' })
  await expect(table(page).locator('tr[data-routine-row]').first()).toContainText('settle_round')

  await total.getByRole('button').click()
  await expect(page).toHaveURL(/order=asc/u)
  await expect(total).toHaveAttribute('aria-sort', 'ascending')
  await expect(total).toContainText('▲')
  await expect.poll(() => lastAsk(asked)).toEqual({ range: '30d', sort: 'total', order: 'asc' })

  const slowest = table(page).locator('th[data-sort="slowest"]')
  await slowest.getByRole('button').click()
  await slowest.getByRole('button').click()
  await expect(slowest).toHaveAttribute('aria-sort', 'ascending')
  await expect(
    table(page).locator('tr[data-routine-row]').first().locator('[data-cell="max"]'),
  ).not.toHaveText('')

  await page.reload()
  await expect(table(page).locator('th[data-sort="slowest"]')).toHaveAttribute(
    'aria-sort',
    'ascending',
  )
})
