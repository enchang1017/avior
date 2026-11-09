import type { Page } from '@playwright/test'

import { administrator, serveFixedApi } from '../visual/api'

export type Environment = (typeof administrator)['environment']

export async function serve(page: Page, environment: Environment = 'production', signedIn = true) {
  const missed = await serveFixedApi(page, signedIn)
  if (signedIn) {
    await page.route('**/api/v1/auth/me', (route) =>
      route.fulfill({ json: { ...administrator, environment } }),
    )
  }
  return missed
}

/** A result as wide and as long as P11 and P17 name: 500 rows of 12 columns. */
export async function serveWideResult(page: Page, rows = 500, columns = 12): Promise<void> {
  const names = Array.from({ length: columns }, (_, index) => `column_${index + 1}`)
  const body = {
    columns: names.map((name) => ({ name, type: 'text', masked: false })),
    rows: Array.from({ length: rows }, (_, row) => names.map((name) => `${name}-${row}`)),
    rowCount: rows,
    truncated: false,
    elapsedMs: 12,
    auditId: '00000000-0000-0000-0000-000000000500',
    estimatedCost: 42,
    estimatedRows: rows,
    limits: { maxCost: 10000, maxRows: 100000 },
  }
  await page.route('**/api/v1/query/execute', (route) => route.fulfill({ json: body }))
}

export async function openMember(page: Page): Promise<void> {
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await page.locator('.cm-content').filter({ hasText: 'LIMIT 100' }).waitFor()
}

/** P16's chart at its cap: a database of 612 objects, drawn as 150. */
export async function serveLargeGraph(page: Page): Promise<void> {
  const { largeGraph } = await import('../redesign/fixtures')
  await page.route('**/api/v1/schema/graph**', (route) => route.fulfill({ json: largeGraph() }))
}

/** P11's chart: collapse it, expand it, select a star, collapse it again. */
export async function driveChart(page: Page): Promise<void> {
  const toggle = page.getByTestId('chart-panel').locator('button.chart-toggle')
  const stars = page.getByTestId('schema-chart').locator('circle.star')
  await stars.first().waitFor()
  await toggle.click()
  await toggle.click()
  await stars.nth(7).click({ force: true })
  await toggle.click()
}

/** P11's sky: one brush across it, in forty steps. */
export async function brushSky(page: Page, steps = 40): Promise<void> {
  const sky = page.getByTestId('audit-sky').locator('svg')
  const box = await sky.boundingBox()
  if (box === null) throw new Error('the sky is not drawn')
  const y = box.y + box.height / 3
  await page.mouse.move(box.x + box.width * 0.2, y)
  await page.mouse.down()
  for (let step = 1; step <= steps; step += 1) {
    await page.mouse.move(box.x + box.width * (0.2 + (0.6 * step) / steps), y)
  }
  await page.mouse.up()
}

/** P20's lineage page at its cap: a focus with 50 neighbours and 99 more beyond them. */
export async function serveCapLineage(page: Page): Promise<string> {
  const { addressOf, capWorld, capFocus } = await import('../visual/lineage')
  const { galaxy, neighbourhood, routes } = await import('../visual/lineageServer')
  const world = capWorld()
  await page.route('**/api/v1/lineage/**', (route) => {
    const url = new URL(route.request().url())
    const reply = url.pathname.endsWith('/lineage/path')
      ? routes(world, url.searchParams)
      : url.pathname.endsWith('/lineage/overview')
        ? galaxy(world)
        : neighbourhood(world, url.searchParams)
    return route.fulfill({ status: reply.status, json: reply.body })
  })
  const query = new URLSearchParams({ database: 'aviorfixture', ...addressOf(capFocus), hops: '2' })
  return `/lineage?${query.toString()}`
}

/** Marks the moment the lineage answer resolved and the moment its stars were first on screen. */
export async function markLineagePaint(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.fetch = async (...args: Parameters<typeof fetch>) => {
      const response = await original(...args)
      const request = args[0]
      const url =
        typeof request === 'string' ? request : request instanceof URL ? request.href : request.url
      if (url.includes('/lineage/graph') && window.__perf.marks['answered'] === undefined) {
        window.__perf.marks['answered'] = performance.now()
      }
      return response
    }
    new MutationObserver((_, observer) => {
      if (document.querySelector('[data-testid="lineage-chart"] [data-node]') === null) return
      observer.disconnect()
      requestAnimationFrame(() =>
        requestAnimationFrame(() => {
          window.__perf.marks['painted'] = performance.now()
        }),
      )
    }).observe(document, { childList: true, subtree: true })
  })
}
