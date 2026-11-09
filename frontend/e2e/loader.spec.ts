import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'

/*
 * FEAT-074: the branded loader where the console boots, where a page's code is
 * still on its way, and the sign-in page it shares a star with.
 */

test.use({ viewport: { width: 1440, height: 900 } })

/** Holds one lazily loaded view back, so its route is pending for as long as asked. */
async function holdBack(page: Page, view: RegExp, ms: number): Promise<void> {
  await page.route(view, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, ms))
    await route.continue()
  })
}

/** The most the route loader was ever seen, frame by frame, from now until it is read. */
async function watchPending(page: Page): Promise<void> {
  await page.evaluate(() => {
    const seen = { max: 0 }
    ;(window as unknown as { __pending: typeof seen }).__pending = seen
    const tick = () => {
      const shown = document.querySelector('.route-pending')
      if (shown !== null) seen.max = Math.max(seen.max, Number(getComputedStyle(shown).opacity))
      requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  })
}

const mostPending = (page: Page) =>
  page.evaluate(() => (window as unknown as { __pending: { max: number } }).__pending.max)

test('FEAT-074 AC-1: index.html draws the loader inside #app with no script of its own, and the console replaces it', async ({
  page,
  request,
}) => {
  const html = await (await request.get('/')).text()
  const app = /<div id="app">([\s\S]*?)<\/div>\s*<script/u.exec(html)?.[1] ?? ''
  expect(app).toContain('class="boot"')
  expect(app).toContain('role="status"')
  expect(app).toContain('<svg class="boot-mark"')
  expect(app).not.toContain('<script')
  expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>\s*\S/u)

  await serveFixedApi(page)
  await page.goto('/')
  await expect(page.locator('.cm-content')).toBeVisible()
  await expect(page.locator('.boot')).toHaveCount(0)
})

test('FEAT-074 AC-1: before the script runs the boot loader is on screen', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false })
  const page = await context.newPage()
  await page.goto('/')
  const loader = page.getByRole('status', { name: 'Loading Avior' })
  await expect(loader).toBeVisible()
  const orb = await page.locator('.boot-orb').boundingBox()
  expect(orb?.width).toBe(160)
  expect(orb?.height).toBe(160)
  await context.close()
})

test('FEAT-074 AC-3: a route pending past 150 ms shows the page loader, and one that loads sooner never does', async ({
  page,
}) => {
  await serveFixedApi(page)
  await holdBack(page, /features\/audit\/AuditLogView\.vue/u, 900)
  await page.goto('/')
  await expect(page.locator('.cm-content')).toBeVisible()
  await watchPending(page)

  await page.locator('a[href="/audit"]').first().click()
  const loader = page.getByRole('status', { name: 'Opening the page' })
  await expect(loader).toBeVisible()
  await expect(loader).toHaveAttribute('data-size', 'page')
  const orb = await loader.locator('.loader-orb').boundingBox()
  expect(orb?.height ?? 0, 'the page loader is page-sized').toBeGreaterThanOrEqual(140)
  expect(orb?.height ?? 0, 'the page loader is page-sized').toBeLessThanOrEqual(180)
  await expect.poll(() => mostPending(page)).toBe(1)
  await expect(page).toHaveURL(/\/audit$/u)
  await expect(page.locator('.route-pending')).toHaveCount(0)

  await page.evaluate(() => {
    ;(window as unknown as { __pending: { max: number } }).__pending.max = 0
  })
  await page.locator('a[href="/"]').first().click()
  await expect(page.locator('.cm-content')).toBeVisible()
  await expect(page.locator('.route-pending')).toHaveCount(0)
  expect(await mostPending(page), 'a fast route never shows the loader').toBe(0)
})

test('FEAT-074 AC-5: the sign-in page has the hero, the tagline, the card with the real logo, and the sky', async ({
  page,
}) => {
  await serveFixedApi(page, false)
  await page.goto('/login')

  const hero = page.locator('section.hero')
  await expect(hero.locator('.hero-star')).toBeVisible()
  await expect(hero.locator('.hero-star')).toHaveAttribute('aria-hidden', 'true')
  await expect(hero.getByText('Epsilon Carinae · the keel')).toBeVisible()
  await expect(
    hero.getByRole('heading', { name: 'Find your way through the sea of data.' }),
  ).toBeVisible()
  await expect(hero.getByText('Every statement guarded, every answer recorded.')).toBeVisible()

  const card = page.locator('form.glow-card')
  const logo = card.getByRole('img', { name: 'Avior' })
  await expect(logo).toBeVisible()
  await expect(logo.locator('svg')).toHaveCount(2)
  await expect(card.getByText('Governed SQL gateway')).toBeVisible()
  await expect(card.getByLabel('Username')).toBeVisible()
  await expect(card.getByLabel(/Password/u)).toBeVisible()
  await expect(card.getByRole('button', { name: 'Sign in' })).toBeVisible()

  const sky = page.locator('.deep-space')
  await expect(sky).toHaveAttribute('aria-hidden', 'true')
  await expect(sky.locator('.space-deep')).toHaveCount(1)
  await expect(sky.locator('.nebula')).toHaveCount(2)
  await expect(sky.locator('.star-field')).toHaveCount(3)
  await expect(sky.locator('.star-glints')).toHaveCount(1)
  await expect(sky.locator('.meteor')).toHaveCount(1)
})

test('FEAT-074 AC-5: the sign-in card sits beside the hero, and alone on a narrow screen', async ({
  page,
}) => {
  await serveFixedApi(page, false)
  await page.goto('/login')
  const hero = await page.locator('section.hero').boundingBox()
  const card = await page.locator('form.glow-card').boundingBox()
  expect(hero).not.toBeNull()
  expect(card?.x ?? 0).toBeGreaterThan((hero?.x ?? 0) + (hero?.width ?? 0))

  await page.setViewportSize({ width: 800, height: 900 })
  await expect(page.locator('section.hero')).toBeHidden()
  await expect(page.locator('form.glow-card')).toBeInViewport()
})
