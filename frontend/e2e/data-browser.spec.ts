import { expect, request, test } from '@playwright/test'
import type { Page } from '@playwright/test'

async function choose(page: Page, id: string, option: string): Promise<void> {
  await page.locator('#' + id).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('data browsing keeps drafts and table state', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('Username').fill('developer')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.locator('.cm-content')).toBeVisible()
  const draft = "SELECT member_id FROM public.member WHERE login_name = 'alice'"
  await page.locator('.cm-content').fill(draft)
  const database = page.locator('.node.database', { hasText: 'aviorfixture' })
  await database.click()
  await page
    .locator('.tables .node')
    .filter({ has: page.locator('.node-label:text-is("member")') })
    .click()
  const data = page.getByRole('region', { name: 'Table data' })
  await expect(data.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await choose(page, 'browse-column', 'login_name')
  await data.getByLabel('Filter value').fill('alice')
  await data.getByRole('button', { name: 'Apply', exact: true }).click()
  await expect(data.getByRole('cell', { name: 'bob', exact: true })).toBeHidden()
  await expect(data.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await page
    .locator('.tables .node')
    .filter({ has: page.locator('.node-label:text-is("bet")') })
    .click()
  await expect(data.getByRole('columnheader', { name: /bet_id/ })).toBeVisible()
  await page
    .locator('.tables .node')
    .filter({ has: page.locator('.node-label:text-is("member")') })
    .click()
  await expect(data.getByLabel('Filter value')).toHaveValue('alice')
  await expect(data.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await expect(data.getByRole('cell', { name: 'bob', exact: true })).toBeHidden()
  await data.getByRole('cell', { name: '1', exact: true }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(data.getByRole('cell', { name: 'alice', exact: true })).toBeFocused()
  await data.getByRole('cell', { name: 'alice', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog')).toContainText('alice')
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
  await page.getByRole('tab', { name: 'SQL', exact: true }).click()
  await expect(page.locator('.cm-content')).toHaveText(draft)
  await page.getByRole('tab', { name: 'Data', exact: true }).click()
  await data.getByRole('button', { name: 'Open in SQL', exact: true }).click()
  await expect(page.locator('.cm-content')).toContainText("login_name = E'alice'")
  await expect(page.locator('.cm-content')).toContainText('OFFSET 0')
  await page.getByRole('tablist', { name: 'Open statements' }).getByRole('tab').first().click()
  await expect(page.locator('.cm-content')).toHaveText(draft)
  await page.getByRole('tab', { name: 'Data', exact: true }).click()
  await page.locator('.node.database', { hasText: 'aviordeploy' }).click()
  await page.getByRole('tab', { name: 'SQL', exact: true }).click()
  await expect(page.locator('.risk .picker .crumb')).toHaveText('aviorfixture')
  const query = page.waitForRequest((request) => request.url().endsWith('/query/execute'))
  await page.getByRole('button', { name: 'Execute', exact: false }).click()
  expect((await query).postDataJSON()).toMatchObject({ database: 'aviorfixture', statement: draft })
})

test('a staged table edit reaches PostgreSQL only after a second person approves it', async ({
  page,
  baseURL,
}) => {
  if (!baseURL) throw new Error('The running stack must have a base URL')
  const developer = await request.newContext({ baseURL })
  const approver = await request.newContext({ baseURL })
  const id = 5000000 + Math.floor(Math.random() * 1000000)
  let seedRequest = ''
  try {
    for (const [api, username] of [
      [developer, 'developer'],
      [approver, 'writeapprover'],
    ] as const) {
      const login = await api.post('/api/v1/auth/login', { data: { username } })
      expect(login.ok(), await login.text()).toBe(true)
    }
    const seeded = await developer.post('/api/v1/writes', {
      data: {
        database: 'aviorfixture',
        statement: `INSERT INTO public.bet (bet_id, member_id, stake, odds, win_lost, status) VALUES (${id}, 1, 10.0000, 1.500, NULL, 'Open')`,
        reason: 'Create an owned grid editing fixture',
      },
    })
    expect(seeded.status(), await seeded.text()).toBe(201)
    seedRequest = ((await seeded.json()) as { id: string }).id
    const seedApproval = await approver.post(`/api/v1/writes/${seedRequest}/decision`, {
      data: { decision: 'approve' },
    })
    expect(seedApproval.ok(), await seedApproval.text()).toBe(true)

    await page.goto('/login')
    await page.getByLabel('Username').fill('developer')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await page.locator('.node.database', { hasText: 'aviorfixture' }).click()
    await page
      .locator('.tables .node')
      .filter({ has: page.locator('.node-label:text-is("bet")') })
      .click()
    const data = page.getByRole('region', { name: 'Table data' })
    await choose(page, 'browse-column', 'bet_id')
    await data.getByLabel('Filter value').fill(String(id))
    await data.getByRole('button', { name: 'Apply', exact: true }).click()
    await expect(data.getByRole('cell', { name: String(id), exact: true })).toBeVisible()
    await data.getByRole('button', { name: 'Edit rows', exact: true }).click()
    await data.locator('.result td').filter({ hasText: /^Open/u }).dblclick()
    const edit = page.getByRole('dialog', { name: 'Edit status', exact: true })
    await edit.getByLabel('New value').fill('Void')
    await edit.getByRole('button', { name: 'Apply to draft' }).click()
    await data.getByRole('button', { name: 'Review 1 change(s)' }).click()
    const review = page.getByRole('dialog', { name: 'Review row changes', exact: true })
    await review.getByLabel('Reason').fill('Verify a complete grid-to-approval flow')
    const submitted = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/writes') && response.request().method() === 'POST',
    )
    await review.getByRole('button', { name: 'Submit for approval', exact: true }).click()
    const pending = (await (await submitted).json()) as { id: string; state: string }
    expect(pending.state).toBe('pending')
    await expect(review).toBeHidden()
    await expect(data.locator('.result td').filter({ hasText: /^Open/u })).toBeVisible()
    const approved = await approver.post(`/api/v1/writes/${pending.id}/decision`, {
      data: { decision: 'approve' },
    })
    expect(approved.ok(), await approved.text()).toBe(true)
    expect(((await approved.json()) as { request: { state: string } }).request.state).toBe(
      'applied',
    )
    await data.getByRole('button', { name: 'Refresh', exact: true }).click()
    await expect(data.locator('.result td').filter({ hasText: /^Void/u })).toBeVisible()
  } finally {
    if (seedRequest) {
      const restored = await approver.post(`/api/v1/writes/${seedRequest}/rollback`, {
        data: { reason: 'Remove the owned grid editing fixture' },
      })
      expect(restored.ok(), await restored.text()).toBe(true)
    }
    await developer.dispose()
    await approver.dispose()
  }
})
