import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { answer, answerJson, calls, record } from './redesign/fixtures'

/*
 * FEAT-065 AC-9b to AC-11 and the review layout of ADR 0012: the update
 * requests are read beside their list, the release queue opens each release on
 * its own page, and every decision sends what it sent before.
 */

const detail = (page: Page) => page.getByTestId('request-detail')
const item = (page: Page, id: string) => page.locator(`[data-request^="${id}"]`)
const pending = 'wr-1111-2222-3333'
const applied = 'wr-4444-5555-6666'

/** Answers a decision with the detail the page last read for the same item, as the server would. */
async function decideAnswers(page: Page, path: string): Promise<void> {
  const detailPath = path.replace(/\/decision$/u, '')
  let lastDetail: unknown = null
  page.on('response', (response) => {
    if (
      response.request().method() === 'GET' &&
      new URL(response.url()).pathname === `/api/v1${detailPath}`
    ) {
      void response.json().then((body: unknown) => {
        lastDetail = body
      })
    }
  })
  await answer(page, path, (route) => route.fulfill({ json: lastDetail }))
}

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
})

test('each request row draws its trajectory', async ({ page }) => {
  await page.goto('/writes')
  await expect(item(page, 'wr-1111').locator('.trajectory .caption')).toHaveText(
    'Awaiting approval',
  )
  await expect(item(page, 'wr-1111').locator('[data-state="current"]')).toHaveCount(1)
  await expect(item(page, 'wr-4444').locator('circle[data-state="done"]')).toHaveCount(4)
  await expect(item(page, 'wr-7777').locator('g[data-state="stopped"]')).toHaveCount(1)
})

test('the waiting list opens its first request beside the list and approves as before', async ({
  page,
}) => {
  await decideAnswers(page, `/writes/${pending}/decision`)
  const seen = record(page)
  await page.goto('/writes/queue')

  await expect(page).toHaveURL(new RegExp(`open=${pending}`, 'u'))
  await expect(item(page, 'wr-1111')).toHaveAttribute('aria-current', 'true')
  await expect(detail(page).locator('li')).toHaveCount(4)
  await expect(detail(page).locator('li[data-state="current"]')).toContainText('Approval')
  await expect(detail(page).locator('.reason')).not.toBeEmpty()
  await expect(detail(page).getByRole('button', { name: 'Reject' })).toBeInViewport()

  await detail(page).getByRole('button', { name: 'Approve and run' }).click()
  await expect.poll(() => calls(seen, 'POST', `/writes/${pending}/decision`).length).toBe(1)
  expect(calls(seen, 'POST', `/writes/${pending}/decision`)[0]?.body).toEqual({
    decision: 'approve',
  })
})

test('a rejection carries its note', async ({ page }) => {
  await decideAnswers(page, `/writes/${pending}/decision`)
  const seen = record(page)
  await page.goto(`/writes?open=${pending}`)
  await detail(page).getByLabel('Note').fill('too broad a WHERE')
  await detail(page).getByRole('button', { name: 'Reject' }).click()
  await expect.poll(() => calls(seen, 'POST', `/writes/${pending}/decision`).length).toBe(1)
  expect(calls(seen, 'POST', `/writes/${pending}/decision`)[0]?.body).toEqual({
    decision: 'reject',
    note: 'too broad a WHERE',
  })
})

test('a link opens its request, and choosing another puts that one in the address', async ({
  page,
}) => {
  await page.goto(`/writes?open=${applied}`)
  await expect(item(page, 'wr-4444')).toHaveAttribute('aria-current', 'true')
  await expect(detail(page).getByRole('heading', { name: '#wr-4444-' })).toBeVisible()
  await expect(detail(page).getByText('To undo it')).toBeVisible()

  await item(page, 'wr-7777').click()
  await expect(page).toHaveURL(/open=wr-7777-8888-9999/u)
  await expect(detail(page).getByRole('heading', { name: '#wr-7777-' })).toBeVisible()
})

test('j and k move through the list, the arrows too, and Enter reads the request', async ({
  page,
}) => {
  await page.goto('/writes')
  await expect(item(page, 'wr-1111')).toBeVisible()
  await expect(detail(page)).toHaveCount(0)

  await page.keyboard.press('j')
  await expect(page).toHaveURL(new RegExp(`open=${pending}`, 'u'))
  await page.keyboard.press('j')
  await expect(page).toHaveURL(new RegExp(`open=${applied}`, 'u'))
  await expect(item(page, 'wr-4444')).toBeFocused()
  await page.keyboard.press('k')
  await expect(item(page, 'wr-1111')).toHaveAttribute('aria-current', 'true')

  await page.keyboard.press('ArrowDown')
  await expect(item(page, 'wr-4444')).toHaveAttribute('aria-current', 'true')
  await page.keyboard.press('ArrowUp')
  await expect(item(page, 'wr-1111')).toHaveAttribute('aria-current', 'true')

  await page.keyboard.press('Enter')
  await expect(detail(page).getByRole('heading', { name: '#wr-1111-' })).toBeFocused()

  // Typing a note is typing, not moving.
  await detail(page).getByLabel('Note').fill('j')
  await expect(item(page, 'wr-1111')).toHaveAttribute('aria-current', 'true')
})

test('a request read once is shown again without asking, until the list is refreshed', async ({
  page,
}) => {
  const seen = record(page)
  await page.goto('/writes')
  await item(page, 'wr-1111').click()
  await expect(detail(page).getByRole('heading', { name: '#wr-1111-' })).toBeVisible()
  await item(page, 'wr-4444').click()
  await expect(detail(page).getByRole('heading', { name: '#wr-4444-' })).toBeVisible()
  await item(page, 'wr-1111').click()
  await expect(detail(page).getByRole('heading', { name: '#wr-1111-' })).toBeVisible()
  await expect(detail(page).getByRole('status', { name: 'Opening the request' })).toHaveCount(0)

  expect(calls(seen, 'GET', `/writes/${pending}`)).toHaveLength(1)
  expect(calls(seen, 'GET', `/writes/${applied}`)).toHaveLength(1)

  await page.getByRole('button', { name: 'Apply' }).click()
  await expect.poll(() => calls(seen, 'GET', `/writes/${pending}`).length).toBe(2)
  await expect(detail(page).getByRole('heading', { name: '#wr-1111-' })).toBeVisible()
  expect(calls(seen, 'GET', `/writes/${applied}`)).toHaveLength(1)
})

/*
 * A statement long enough to scroll is read in one scroll, the detail's, and
 * the decision stays on screen at the top of it and at the bottom.
 */
test('a long statement scrolls the detail once, and the decision never leaves the screen', async ({
  page,
}) => {
  const lines = Array.from({ length: 90 }, (_, n) => `  AND bet_id <> ${n + 1000}`).join('\n')
  const statement = `UPDATE public.bet SET status = 'Void' WHERE member_id = 42\n${lines}`
  await answerJson(page, `/writes/${pending}`, {
    request: {
      id: pending,
      database: 'aviorfixture',
      statement,
      kind: 'update',
      targetTable: 'public.bet',
      reason: 'A statement longer than the screen.',
      requestedBy: 'chen.yu',
      requestedAt: '2026-09-20T08:30:00Z',
      estimatedRows: 1,
      state: 'pending',
      reversible: false,
    },
    history: [],
  })
  await page.goto(`/writes?open=${pending}`)
  const approve = detail(page).getByRole('button', { name: 'Approve and run' })
  await expect(detail(page).locator('.cm-content')).toContainText('bet_id <> 1000')

  const scroller = detail(page).locator('.cm-scroller').first()
  const inner = await scroller.evaluate((element) => element.scrollHeight - element.clientHeight)
  expect(inner, 'the statement box scrolls on its own').toBeLessThanOrEqual(1)
  await expect(approve).toBeInViewport()

  await detail(page)
    .locator('[data-scroll]')
    .evaluate((element) => element.scrollTo({ top: element.scrollHeight }))
  await expect(detail(page).locator('.cm-content')).toContainText('bet_id <> 1089')
  await expect(detail(page).getByText('Approval steps')).toBeInViewport()
  await expect(approve).toBeInViewport()
})

test('a caller who cannot decide sees no decision', async ({ page }) => {
  await page.route('**/api/v1/auth/me', (route) =>
    route.fulfill({
      json: {
        id: 'op-dev',
        username: 'developer',
        roles: ['developer'],
        permissions: ['query.inspect', 'query.execute', 'audit.read.own'],
        features: ['write', 'release'],
        environment: 'staging',
        selfApproval: false,
        instance: { id: 'local', name: 'Local PostgreSQL' },
      },
    }),
  )
  await page.goto(`/writes?open=${pending}`)
  await expect(detail(page).locator('li')).toHaveCount(4)
  await expect(detail(page).getByRole('button', { name: /Approve/u })).toHaveCount(0)
  await expect(detail(page).getByRole('button', { name: 'Reject' })).toHaveCount(0)
})

test('on a narrow screen the list and the request take turns', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 900 })
  await page.goto('/writes/queue')
  await expect(item(page, 'wr-1111')).toBeVisible()
  await expect(page).not.toHaveURL(/open=/u)

  await item(page, 'wr-1111').click()
  await expect(detail(page).getByRole('heading', { name: '#wr-1111-' })).toBeVisible()
  await expect(item(page, 'wr-1111')).toBeHidden()

  await detail(page).getByRole('button', { name: 'All requests' }).click()
  await expect(item(page, 'wr-1111')).toBeVisible()
  await expect(page).not.toHaveURL(/open=/u)
})

test('a release row opens the release on its own page, by click or by Enter', async ({ page }) => {
  await page.goto('/releases')
  const row = page.locator('tbody tr').filter({ hasText: 'pending' }).first()
  await expect(row.locator('.trajectory .caption')).toHaveText('Awaiting approval')
  await row.locator('td.database').click()
  await expect(page).toHaveURL(/\/releases\/rel-0001-aaaa-bbbb$/u)
  await expect(page.locator('.approval-steps li')).toHaveCount(4)
  await expect(page.getByText('20260920_0001_add_member_tier.sql')).toBeVisible()

  await page.goBack()
  await page.getByRole('link', { name: 'Review' }).first().focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/releases\/rel-0001-aaaa-bbbb$/u)
})

test('a release is decided on its page as before', async ({ page }) => {
  await decideAnswers(page, '/releases/rel-0001-aaaa-bbbb/decision')
  const seen = record(page)
  await page.goto('/releases/rel-0001-aaaa-bbbb')
  await page.getByRole('button', { name: 'Approve', exact: true }).click()
  await expect
    .poll(() => calls(seen, 'POST', '/releases/rel-0001-aaaa-bbbb/decision').length)
    .toBe(1)
  expect(calls(seen, 'POST', '/releases/rel-0001-aaaa-bbbb/decision')[0]?.body).toEqual({
    decision: 'approve',
  })
})
