import { expect, test, type Page } from '@playwright/test'

import { frameWork, infiniteAnimations, nextPaint, observe } from './perf/measure'
import { animationName, budget } from './perf/budget'
import { serve } from './perf/scene'

/*
 * FEAT-074 AC-9 to AC-11 against ADR 0009: the boot loader, the route loader
 * and the sign-in page, on the production build, measured the way P13, P15
 * and P10 are measured for the rest of the console.
 */

test.use({ viewport: { width: 1440, height: 900 }, timezoneId: 'UTC', locale: 'en-US' })

/** The page as index.html draws it: no script of the build's ever arrives. */
async function bootOnly(page: Page): Promise<void> {
  await page.route('**/assets/*.js', (route) => route.abort())
  await page.goto('/')
  await expect(page.getByRole('status', { name: 'Loading Avior' })).toBeVisible()
}

async function signIn(page: Page): Promise<void> {
  await serve(page, 'production', false)
  await page.goto('/login')
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
  await page.mouse.move(0, 899)
  await page.waitForTimeout(1500)
}

/** Holds a view's chunk back, so opening it keeps its route pending for as long as asked. */
async function holdBack(page: Page, chunk: RegExp, ms: number): Promise<void> {
  await page.route(chunk, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, ms))
    await route.continue()
  })
}

/** Where a comet is and how it is turned, read off the page. */
async function comet(page: Page, orb: string, turning: string, head: string) {
  return page.evaluate(
    ({ orb, turning, head }) => {
      const box = document.querySelector(orb)?.getBoundingClientRect()
      const dot = document.querySelector(head)?.getBoundingClientRect()
      const layer = document.querySelector(turning)
      if (box === undefined || dot === undefined || layer === null) return null
      return {
        transform: getComputedStyle(layer).transform,
        animation: getComputedStyle(layer).animationName,
        dx: Math.round(dot.x + dot.width / 2 - (box.x + box.width / 2)),
        dy: Math.round(dot.y + dot.height / 2 - (box.y + box.height / 2)),
      }
    },
    { orb, turning, head },
  )
}

test('FEAT-074 AC-10: the boot loader does no layout or paint over the quiet window', async ({
  page,
}) => {
  await bootOnly(page)
  await page.waitForTimeout(500)
  const moving = (await infiniteAnimations(page)).map(animationName)
  const work = await frameWork(page, budget.quietWindowMs)
  console.warn(
    `boot loader layouts ${work.layouts} · paints ${work.paints} over ${budget.quietWindowMs} ms` +
      ` · infinite ${moving.length}: ${moving.join(', ')}`,
  )
  expect(moving, 'the comet is what moves').toEqual(['boot-orbit'])
  expect(work.layouts, 'P13 Layout events').toBe(0)
  expect(work.paints, 'P13 Paint events').toBe(0)
})

test('FEAT-074 AC-10: the sign-in page does no layout or paint and stays within P15', async ({
  page,
}) => {
  await signIn(page)
  const moving = (await infiniteAnimations(page)).map(animationName)
  const work = await frameWork(page, budget.quietWindowMs)
  console.warn(
    `sign-in layouts ${work.layouts} · paints ${work.paints} over ${budget.quietWindowMs} ms` +
      ` · infinite ${moving.length}: ${moving.join(', ')}`,
  )
  expect(moving).toEqual(
    expect.arrayContaining(['orbit', 'drift-a', 'drift-b', 'pan', 'twinkle', 'meteor']),
  )
  expect(moving.length, 'P15 infinite animations at once').toBeLessThanOrEqual(
    budget.motion.maxInfiniteAnimations,
  )
  expect(work.layouts, 'P13 Layout events').toBe(0)
  expect(work.paints, 'P13 Paint events').toBe(0)
})

test('FEAT-074 AC-11: the console replacing the boot loader, and a page replacing the route loader, shift the layout by at most 0.02', async ({
  page,
}) => {
  await serve(page)
  await observe(page)
  await holdBack(page, /\/assets\/AuditLogView-[\w-]+\.js$/u, 900)
  await page.goto('/')
  await expect(page.locator('.cm-content')).toBeVisible()
  await expect(page.locator('.boot')).toHaveCount(0)
  await nextPaint(page)
  const booted = await page.evaluate(() => window.__perf.cls)

  await page.locator('a[href="/audit"]').first().click()
  await expect(page.getByRole('status', { name: 'Opening the page' })).toBeVisible()
  await expect(page).toHaveURL(/\/audit$/u)
  await expect(page.locator('.route-pending')).toHaveCount(0)
  await page.waitForLoadState('networkidle')
  await nextPaint(page)
  const routed = await page.evaluate(() => window.__perf.cls)

  const round = (value: number) => Math.round(value * 1000) / 1000
  console.warn(`CLS boot ${round(booted)} · route loader ${round(routed - booted)}`)
  expect(booted, 'CLS when the console replaces the boot loader').toBeLessThanOrEqual(budget.cls)
  expect(routed - booted, 'CLS when the page replaces the route loader').toBeLessThanOrEqual(
    budget.cls,
  )
})

test('FEAT-074 AC-9: under reduced motion no loader moves, its comet parked at twelve o’clock', async ({
  browser,
}) => {
  const calm = await browser.newPage({ reducedMotion: 'reduce' })

  await bootOnly(calm)
  const boot = await comet(calm, '.boot-orb', '.boot-comet', '.boot-head')
  await calm.waitForTimeout(800)
  expect(await comet(calm, '.boot-orb', '.boot-comet', '.boot-head')).toEqual(boot)
  expect(boot?.animation).toBe('none')
  expect(boot?.transform).toBe('none')
  expect(Math.abs(boot?.dx ?? Infinity), 'the head is on the vertical through the star').toBe(0)
  expect(boot?.dy ?? 0, 'the head is above the star').toBeLessThan(-60)

  await calm.unroute('**/assets/*.js')
  await serve(calm)
  await holdBack(calm, /\/assets\/AuditLogView-[\w-]+\.js$/u, 2500)
  await calm.goto('/')
  await expect(calm.locator('.cm-content')).toBeVisible()
  await calm.locator('a[href="/audit"]').first().click()
  await expect(calm.getByRole('status', { name: 'Opening the page' })).toBeVisible()
  const pending = [
    '.route-pending .loader-orb',
    '.route-pending .comet',
    '.route-pending .comet-head',
  ]
  const [orb = '', turning = '', head = ''] = pending
  const first = await comet(calm, orb, turning, head)
  await calm.waitForTimeout(800)
  expect(await comet(calm, orb, turning, head)).toEqual(first)
  expect(first?.animation).toBe('none')
  expect(first?.transform).toBe('none')
  expect(Math.abs(first?.dx ?? Infinity)).toBe(0)
  expect(first?.dy ?? 0).toBeLessThan(-60)

  await calm.unrouteAll()
  await serve(calm, 'production', false)
  await calm.goto('/login')
  await expect(calm.getByRole('button', { name: 'Sign in' })).toBeVisible()
  await calm.waitForTimeout(500)
  expect(await infiniteAnimations(calm), 'nothing on the sign-in page moves').toEqual([])
  await calm.close()
})
