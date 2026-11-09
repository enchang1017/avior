import { expect, test, type Page, type Route } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { answer, calls, record, result, verdict } from './redesign/fixtures'

const gauge = (page: Page) => page.getByTestId('cost-gauge')
const showPlan = (page: Page) => page.getByRole('tab', { name: 'Plan' }).click()

async function pointAtMember(page: Page): Promise<void> {
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/u }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(page.locator('.cm-content')).toContainText('LIMIT 100')
}

function queue(
  page: Page,
  path: string,
  answers: { status?: number; body: unknown }[],
): Promise<void> {
  return answer(page, path, (route: Route) => {
    const next = answers.shift() ?? {
      body: { code: 'none', message: 'no more answers' },
      status: 500,
    }
    return route.fulfill({ status: next.status ?? 200, json: next.body })
  })
}

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
})

test('the gauge reads cost against the cap and warns near it', async ({ page }) => {
  await queue(page, '/query/inspect', [{ body: verdict(312) }, { body: verdict(8_500) }])
  await queue(page, '/query/execute', [
    {
      status: 403,
      body: verdict(12_000, {
        decision: 'deny',
        reasons: [
          {
            rule: 'cost_limit_exceeded',
            message: 'Estimated cost 12000 exceeds the limit of 10000.',
          },
        ],
      }),
    },
    { body: result(40) },
  ])
  await pointAtMember(page)

  await page.getByRole('button', { name: /Inspect/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-level', 'clear')
  await expect(gauge(page).locator('.cost')).toHaveText('312 / 10,000')
  await expect(gauge(page).locator('.rows')).toHaveText('3 / 100,000')
  await expect(gauge(page).locator('.fill')).toHaveAttribute('style', /scaleX\(0\.0312\)/u)

  await page.getByRole('button', { name: /Inspect/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-level', 'near')

  await page.getByRole('button', { name: /Execute/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-level', 'over')
  await expect(gauge(page).locator('.rule')).toHaveText('cost_limit_exceeded')
  await expect(gauge(page).locator('.cost')).toHaveText('12,000 / 10,000')

  await page.getByRole('button', { name: /Execute/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-level', 'clear')
  await expect(gauge(page).locator('.cost')).toHaveText('40 / 10,000')
})

test('typing measures nothing and an edited statement reads stale', async ({ page }) => {
  await queue(page, '/query/inspect', [{ body: verdict(312) }, { body: verdict(null) }])
  const seen = record(page)
  await pointAtMember(page)
  expect(calls(seen, 'POST', '/query/inspect')).toHaveLength(0)

  await page.getByRole('button', { name: /Inspect/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-stale', 'false')

  await page.locator('.cm-content').click()
  await page.keyboard.press('End')
  await page.keyboard.type(' OFFSET 10', { delay: 20 })
  await page.waitForTimeout(400)
  expect(calls(seen, 'POST', '/query/inspect')).toHaveLength(1)
  await expect(gauge(page)).toHaveAttribute('data-stale', 'true')
  await expect(gauge(page)).toContainText('stale')

  await page.getByRole('button', { name: /Inspect/u }).click()
  await showPlan(page)
  await expect(gauge(page)).toHaveAttribute('data-level', 'no-plan')
  await expect(gauge(page).locator('.cost')).toHaveText('no plan')
  await expect(gauge(page).locator('.rows')).toHaveText('no plan')
  await expect(gauge(page)).toHaveAttribute('data-stale', 'false')
})

test('the gauge row sits on the same inset as the plan below it', async ({ page }) => {
  await answer(page, '/query/inspect', (route) => route.fulfill({ json: verdict(312) }))
  await pointAtMember(page)
  await page.getByRole('button', { name: /Inspect/u }).click()
  await showPlan(page)
  const label = gauge(page).getByText('Plan cost', { exact: true })
  const verdictText = page.locator('.verdict > div').first()
  await expect(verdictText).toBeVisible()
  const [labelBox, verdictBox, paneBox] = await Promise.all([
    label.boundingBox(),
    verdictText.boundingBox(),
    page.locator('.pane-plan').boundingBox(),
  ])
  expect(labelBox?.x).toBe(verdictBox?.x)
  expect((labelBox?.x ?? 0) - (paneBox?.x ?? 0)).toBeGreaterThanOrEqual(16)
  expect(labelBox?.y).toBeGreaterThanOrEqual((paneBox?.y ?? 0) + 12)
})
