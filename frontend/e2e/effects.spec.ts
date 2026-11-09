import { expect, test, type Page } from '@playwright/test'

import { animationName, budget } from './perf/budget'
import { frameWork, infiniteAnimations } from './perf/measure'
import { serve } from './perf/scene'

/*
 * The infinite animations the console may run at once: the beacon's own,
 * the chart's target ring, and at most one that an interaction starts (a
 * hovered request's pulse or the open sheet's), all within the P15 cap. Each
 * costs no Layout and no Paint while it runs (ADR 0009 P13).
 */
const beacon = new Set(['beacon-pulse', 'beacon-sweep', 'beacon-travel'])
const ambient = new Set(['target-ring'])
const quietWindowMs = 2000
const hoverEaseMs = 400

const infinite = async (page: Page) => (await infiniteAnimations(page)).map(animationName)

function composition(names: string[]) {
  return {
    beacon: names.filter((name) => beacon.has(name)).length,
    ambient: names.filter((name) => ambient.has(name)).length,
    interaction: names.filter((name) => !beacon.has(name) && !ambient.has(name)).length,
  }
}

function withinBudget(names: string[]): void {
  const parts = composition(names)
  expect(parts.ambient, `ambient animations in ${names.join(', ')}`).toBeLessThanOrEqual(1)
  expect(parts.interaction, `interaction animations in ${names.join(', ')}`).toBeLessThanOrEqual(1)
  expect(names.length, 'P15 infinite animations at once').toBeLessThanOrEqual(
    budget.motion.maxInfiniteAnimations,
  )
}

async function aimedConsole(page: Page): Promise<void> {
  await serve(page, 'production')
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/u }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(page.getByTestId('schema-chart').locator('circle[data-ring="0"]')).toHaveCount(1)
  await page.mouse.move(0, 999)
  await page.waitForTimeout(1200)
}

test('the chart ring joins the beacon and nothing else idles', async ({ page }) => {
  await aimedConsole(page)
  const names = await infinite(page)
  expect(names).toContain('target-ring')
  withinBudget(names)
})

test('the chart ring costs no layout or paint per frame', async ({ page }) => {
  await aimedConsole(page)
  const work = await frameWork(page, quietWindowMs)
  console.warn(`P13 ring  layouts ${work.layouts} · paints ${work.paints} over ${quietWindowMs} ms`)
  expect(work.layouts, 'P13 Layout events').toBe(0)
  expect(work.paints, 'P13 Paint events').toBe(0)
})

test('a release pulses only while it is pointed at', async ({ page }) => {
  await serve(page, 'production')
  await page.goto('/releases')
  const row = page.locator('tbody tr').filter({ hasText: 'pending' }).first()
  await expect(row.locator('.trajectory')).toBeVisible()
  await page.mouse.move(0, 999)
  expect(composition(await infinite(page)).interaction).toBe(0)

  await row.hover()
  const hovered = await infinite(page)
  expect(hovered).toContain('pulse')
  withinBudget(hovered)
  // The row's own hover colour eases in over 120 ms and repaints while it does,
  // once. What is measured here is the pulse that keeps going after it.
  await page.waitForTimeout(hoverEaseMs)
  const work = await frameWork(page, quietWindowMs)
  console.warn(`P13 row pulse  layouts ${work.layouts} · paints ${work.paints}`)
  expect(work.layouts, 'P13 Layout events').toBe(0)
  expect(work.paints, 'P13 Paint events').toBe(0)
})

/*
 * The request being read carries the one pulse, on its current step, and the
 * list beside it adds none: a queue of waiting requests is a column of amber
 * stars, never a column of animations.
 */
test('the request being read pulses once, and pointing at the list beside it adds nothing', async ({
  page,
}) => {
  await serve(page, 'production')
  await page.goto('/writes/queue')
  await expect(page.getByTestId('request-detail').locator('li[data-state="current"]')).toBeVisible()
  await page.mouse.move(0, 999)
  const reading = await infinite(page)
  expect(composition(reading).interaction).toBe(1)
  withinBudget(reading)

  await page.locator('[data-request]').first().hover()
  const pointed = await infinite(page)
  expect(composition(pointed).interaction).toBe(1)
  withinBudget(pointed)
})

test('reduced motion stills the ring and the pulse', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await aimedConsole(page)
  expect(await infinite(page)).toEqual([])
  await page.goto('/releases')
  await page.locator('tbody tr').filter({ hasText: 'pending' }).first().hover()
  expect(await infinite(page)).toEqual([])
  await page.goto('/writes/queue')
  await expect(page.getByTestId('request-detail').locator('li[data-state="current"]')).toBeVisible()
  expect(await infinite(page)).toEqual([])
})
