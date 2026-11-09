import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { answerJson, calls, histogram, record, weekStart } from './redesign/fixtures'

const hour = 3_600_000
const sky = (page: Page) => page.getByTestId('audit-sky').locator('svg')

/** Drags from one bucket to another, with a few moves in between, and releases. */
async function drag(page: Page, from: number, to: number): Promise<void> {
  const box = await sky(page).boundingBox()
  if (box === null) throw new Error('the sky is not drawn')
  const x = (bucket: number) => box.x + ((bucket + 0.5) / 168) * box.width
  const y = box.y + box.height / 3
  await page.mouse.move(x(from), y)
  await page.mouse.down()
  for (let step = 1; step <= 6; step += 1) {
    await page.mouse.move(x(from + ((to - from) * step) / 6), y)
  }
  await page.mouse.up()
}

const iso = (bucket: number) => new Date(weekStart + bucket * hour).toISOString()

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
  await answerJson(page, '/audit/mine/histogram', histogram())
  await answerJson(page, '/audit/histogram', histogram())
})

test('brushing the sky narrows the log and survives a reload', async ({ page }) => {
  const seen = record(page)
  await page.goto('/audit')
  await expect(sky(page)).toBeVisible()

  const beforeDrag = seen.length
  await drag(page, 40, 45)
  await expect.poll(() => calls(seen.slice(beforeDrag), 'GET', '/audit/mine').length).toBe(1)
  const narrowed = calls(seen.slice(beforeDrag), 'GET', '/audit/mine')[0]
  expect(narrowed?.query.get('from')).toBe(iso(40))
  expect(narrowed?.query.get('to')).toBe(
    new Date(weekStart + 46 * hour - 1).toISOString().replace(/Z$/u, '999Z'),
  )
  await expect.poll(() => new URL(page.url()).searchParams.get('from')).toBe(iso(40))
  await expect(page.getByTestId('audit-sky').locator('rect.brush')).toHaveAttribute(
    'data-first',
    '40',
  )

  const before = seen.length
  await page.reload()
  await expect(page.getByTestId('audit-sky').locator('rect.brush')).toHaveAttribute(
    'data-first',
    '40',
  )
  await expect(page.getByTestId('audit-sky').locator('rect.brush')).toHaveAttribute(
    'data-last',
    '45',
  )
  await expect
    .poll(() => calls(seen.slice(before), 'GET', '/audit/mine').at(-1)?.query.get('from'))
    .toBe(iso(40))

  const beforeClear = seen.length
  await page.getByRole('button', { name: 'Clear' }).click()
  await expect(page).not.toHaveURL(/from=/u)
  await expect(page.getByTestId('audit-sky').locator('rect.brush')).toHaveCount(0)
  await expect.poll(() => calls(seen.slice(beforeClear), 'GET', '/audit/mine').length).toBe(1)
  expect(calls(seen.slice(beforeClear), 'GET', '/audit/mine')[0]?.query.get('from')).toBeNull()
})

test('a brush drag asks for the log once, on release', async ({ page }) => {
  const seen = record(page)
  await page.goto('/audit')
  await expect(sky(page)).toBeVisible()
  await expect.poll(() => calls(seen, 'GET', '/audit/mine').length).toBeGreaterThan(0)
  const settled = seen.length

  const box = await sky(page).boundingBox()
  if (box === null) throw new Error('the sky is not drawn')
  const y = box.y + box.height / 3
  await page.mouse.move(box.x + box.width * 0.2, y)
  await page.mouse.down()
  for (let step = 1; step <= 10; step += 1)
    await page.mouse.move(box.x + box.width * (0.2 + step * 0.03), y)
  await page.waitForTimeout(300)
  expect(seen.slice(settled)).toEqual([])

  await page.mouse.up()
  await expect.poll(() => calls(seen.slice(settled), 'GET', '/audit/mine').length).toBe(1)
  await page.waitForTimeout(300)
  expect(seen.slice(settled).map((call) => call.path)).toEqual(['/audit/mine'])
})
