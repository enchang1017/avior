import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { serveObservatory } from './visual/observatory'

/*
 * FEAT-072's overview against the fixed API, read the way an operator reads
 * it: the cards, the charts, their tooltips and drill-downs, the address, the
 * refresh and the motion.
 */

test.use({ viewport: { width: 1440, height: 900 }, timezoneId: 'UTC', locale: 'en-US' })

async function open(page: Page, caller = 'administrator', query = '') {
  const missed = await serveFixedApi(page)
  await serveObservatory(page, caller)
  await page.goto(`/observatory${query}`)
  await expect(page.getByTestId('overview-kpis')).toBeVisible()
  return missed
}

const kpis = (page: Page) => page.getByTestId('overview-kpis').locator('[data-testid="kpi-value"]')
const chart = (page: Page, name: string) => page.locator(`[data-chart="${name}"]`)
const scroller = (page: Page) => page.getByTestId('overview-scroll')

test('FEAT-072 AC-6: the observatory opens on Overview', async ({ page }) => {
  const missed = await open(page, 'dba')
  await expect(page.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
  expect(missed).toEqual([])
})

test('FEAT-072 AC-7: the range, database and refresh are in the address and survive a reload', async ({
  page,
}) => {
  await open(page)
  await page.locator('#overview-range').click()
  await page.getByRole('option', { name: 'Last 30 days' }).click()
  await expect(page).toHaveURL(/range=30d/u)
  await page.locator('#overview-refresh').click()
  await page.getByRole('option', { name: /30 s/u }).click()
  await expect(page).toHaveURL(/refresh=30/u)
  await page.reload()
  await expect(page.locator('#overview-range')).toHaveValue('Last 30 days')
  await expect(page.locator('#overview-refresh')).toHaveValue(/30 s/u)
})

test('FEAT-072 AC-8: a developer lands on the whole dashboard', async ({ page }) => {
  await open(page, 'developer')
  await expect(kpis(page)).toHaveCount(6)
  for (const name of ['volume', 'latency']) await expect(chart(page, name)).toBeVisible()
  await scroller(page).focus()
  await page.keyboard.press('End')
  for (const name of ['size', 'operators', 'reasons', 'tables', 'idle', 'slowest']) {
    await expect(chart(page, name)).toBeVisible()
  }
  await expect(page.getByTestId('live-panel')).toBeVisible()
  await expect(page.getByText('Usage figures are your own statements.')).toHaveCount(0)
})

test('FEAT-072 AC-9: a chart shows exact values on hover and on focus, and Enter drills in', async ({
  page,
}) => {
  await open(page)
  const plot = chart(page, 'volume').getByTestId('chart-plot')
  const box = await plot.boundingBox()
  if (box === null) throw new Error('the volume chart has no box')
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await expect(chart(page, 'volume').getByTestId('chart-tooltip')).toBeVisible()
  await page.mouse.move(5, 895)

  await plot.focus()
  await page.keyboard.press('ArrowRight')
  await expect(chart(page, 'volume').getByTestId('chart-spoken')).not.toHaveText('')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/tab=usage/u)
})

test('FEAT-072 AC-9: the idle card opens Last used on the flagged objects', async ({ page }) => {
  await open(page)
  await page
    .getByTestId('overview-kpis')
    .getByRole('link', { name: /Objects idle/u })
    .click()
  await expect(page).toHaveURL(/tab=last-used/u)
  await expect(page).toHaveURL(/flagged=1/u)
})

test('FEAT-072 AC-10: every chart has a caption, and no card stays loading once answered', async ({
  page,
}) => {
  await open(page)
  await expect(page.getByTestId('overview-kpis').getByTestId('kpi-skeleton')).toHaveCount(0)
  await expect(chart(page, 'volume').locator('figcaption')).toContainText(
    'Statements through Avior per 6 hours',
  )
  await expect(chart(page, 'latency').getByTestId('chart-summary')).not.toHaveText('')
})

test('FEAT-072 AC-11: auto-refresh re-reads only what is live, and nothing when off', async ({
  page,
}) => {
  await page.clock.install({ time: new Date('2026-09-27T12:00:00Z') })
  await open(page)
  await expect(chart(page, 'slowest')).toBeVisible()
  await page.waitForLoadState('networkidle')
  const asked: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path.includes('/observatory/')) asked.push(path.replace(/^.*\/observatory/u, ''))
  })
  await page.clock.runFor(65_000)
  expect(asked).toEqual([])

  await page.locator('#overview-refresh').click()
  await page.getByRole('option', { name: /30 s/u }).click()
  asked.length = 0
  await page.clock.runFor(31_000)
  await expect.poll(() => asked.length).toBeGreaterThan(0)
  expect(new Set(asked)).toEqual(new Set(['/activity']))
})

test('FEAT-072 AC-12: under reduced motion no chart animates', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await open(page)
  await expect(chart(page, 'volume').getByTestId('chart-plot')).toBeVisible()
  const running = await page.evaluate(
    () =>
      document.getAnimations().filter((animation) => {
        const target = (animation.effect as KeyframeEffect | null)?.target
        return (
          animation.playState === 'running' &&
          target instanceof Element &&
          target.closest('[data-chart], [data-testid="overview-kpis"]') !== null
        )
      }).length,
  )
  expect(running).toBe(0)
})

test('FEAT-072 AC-12a: the overview scrolls with the keyboard and the wheel, and lazy charts load', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 600 })
  await open(page)
  await expect(chart(page, 'slowest')).toHaveCount(0)
  await scroller(page).focus()
  await page.keyboard.press('End')
  await expect.poll(() => scroller(page).evaluate((node) => node.scrollTop)).toBeGreaterThan(0)
  await expect(chart(page, 'slowest')).toBeVisible()
  await page.waitForLoadState('networkidle')
  // The lazy charts grow the page after End has aimed at the old bottom, and
  // Home pressed while that scroll is still moving races it. End again until
  // the scroll rests at the bottom of the grown page, then press Home.
  const fromBottom = () =>
    scroller(page).evaluate((node) => node.scrollHeight - node.clientHeight - node.scrollTop)
  await expect(async () => {
    await page.keyboard.press('End')
    await expect.poll(fromBottom, { timeout: 1000 }).toBeLessThanOrEqual(1)
  }).toPass()

  await page.keyboard.press('Home')
  await expect.poll(() => scroller(page).evaluate((node) => node.scrollTop)).toBe(0)
  const box = await scroller(page).boundingBox()
  if (box === null) throw new Error('the overview has no box')
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.wheel(0, 600)
  await expect.poll(() => scroller(page).evaluate((node) => node.scrollTop)).toBeGreaterThan(0)
})

test('FEAT-072 AC-12b: the latency axis is logarithmic and its tooltip is exact', async ({
  page,
}) => {
  await open(page)
  await expect(chart(page, 'latency').getByTestId('chart-scale')).toContainText('Logarithmic')
  const plot = chart(page, 'latency').getByTestId('chart-plot')
  await plot.focus()
  await page.keyboard.press('ArrowRight')
  await expect(chart(page, 'latency').getByTestId('chart-spoken')).toContainText('ms')
})
