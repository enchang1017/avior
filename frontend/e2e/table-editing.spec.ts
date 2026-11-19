import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { openEditableTable, originalJSON, serveEditableTable } from './visual/editing'

async function mockClipboard(page: Page): Promise<void> {
  await page.addInitScript(() =>
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (text: string) => {
          localStorage.setItem('test.clipboard', text)
          return Promise.resolve()
        },
      },
    }),
  )
}

test('JSON tree and code preserve numeric tokens and copy original, formatted, selected and path values', async ({
  page,
}) => {
  await serveEditableTable(page)
  await mockClipboard(page)
  await openEditableTable(page)
  const cell = page.locator('.result tr[data-row]').first().locator('td').nth(2)
  await cell.focus()
  await cell.getByRole('button', { name: 'Inspect payload', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'payload', exact: true })
  await expect(dialog.getByRole('tab', { name: 'Tree', exact: true })).toBeVisible()
  await expect(dialog.getByRole('region', { name: 'JSON fields', exact: true })).toContainText(
    '9007199254740993',
  )
  await dialog.getByRole('button', { name: 'Copy original', exact: true }).click()
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
    .toBe(originalJSON)
  await dialog.getByRole('button', { name: 'Copy formatted', exact: true }).click()
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
    .toContain('9007199254740993')
  await dialog
    .getByRole('region', { name: 'JSON fields', exact: true })
    .getByRole('button', { name: 'id: 9007199254740993', exact: true })
    .click()
  await dialog.getByRole('button', { name: 'Copy path', exact: true }).click()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('test.clipboard'))).toBe('/id')
  await dialog.getByRole('button', { name: 'Copy selection', exact: true }).click()
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
    .toBe('9007199254740993')
  await dialog.getByRole('tab', { name: 'Code', exact: true }).click()
  await expect(dialog.locator('.cm-content')).toContainText('9007199254740993')
  const content = await dialog.locator('.cm-content').textContent()
  await dialog.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.insertText('replacement')
  await expect.poll(() => dialog.locator('.cm-content').textContent()).toBe(content)
})

test('edits stay staged across pages and SQL, then save with a full original snapshot', async ({
  page,
}) => {
  const records = await serveEditableTable(page)
  const writes: unknown[] = []
  await page.route('**/api/v1/writes', async (route) => {
    writes.push(route.request().postDataJSON())
    if (records[0]) records[0][1] = 'alice edited'
    await route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', state: 'applied' } })
  })
  await openEditableTable(page)
  const cell = () => page.locator('.result tr[data-row]').first().locator('td').nth(1)
  await cell().dblclick()
  const editor = page.getByRole('dialog', { name: 'Edit username', exact: true })
  await editor.getByLabel('New value').fill('alice edited')
  await editor.getByRole('button', { name: 'Apply to draft' }).click()
  expect(writes).toHaveLength(0)
  await expect(cell()).toContainText('alice edited')
  await page
    .getByRole('region', { name: 'Table data' })
    .getByRole('button', { name: 'Next', exact: true })
    .click()
  await page
    .getByRole('region', { name: 'Table data' })
    .getByRole('button', { name: 'Previous', exact: true })
    .click()
  await expect(cell()).toContainText('alice edited')
  await page.getByRole('tab', { name: 'SQL', exact: true }).click()
  await page.getByRole('tab', { name: 'Data', exact: true }).click()
  await expect(cell()).toContainText('alice edited')
  await page.getByRole('button', { name: 'Review 1 change(s)' }).click()
  const review = page.getByRole('dialog', { name: 'Review row changes', exact: true })
  await expect(review).toContainText('9007199254740993')
  await review.getByLabel('Reason').fill('correct a fixture name')
  const template = page.waitForRequest((request) => request.url().endsWith('/writes/row-template'))
  await review.getByRole('button', { name: 'Check again' }).click()
  expect((await template).postDataJSON()).toMatchObject({
    original: { id: '9007199254740993', username: 'alice', payload: originalJSON, note: '' },
    changes: { username: 'alice edited' },
  })
  await review.getByRole('button', { name: 'Save changes', exact: true }).click()
  await expect(review).toBeHidden()
  expect(writes).toHaveLength(1)
  expect(writes[0]).toMatchObject({ expectedAffectedRows: 1, reason: 'correct a fixture name' })
  await expect(
    page.getByRole('alert').filter({ hasText: '1 saved, 0 awaiting approval, 0 unsaved.' }),
  ).toBeVisible()
})

test('JSON edits use the current editor content and conflicts retain the pending row', async ({
  page,
}) => {
  await serveEditableTable(page, true)
  await page.route('**/api/v1/writes', (route) =>
    route.fulfill({
      json: {
        id: '00000000-0000-0000-0000-000000000002',
        state: 'failed',
        error: 'write conflict: reload the row and review your changes',
      },
    }),
  )
  await openEditableTable(page)
  const cell = page.locator('.result tr[data-row]').first().locator('td').nth(2)
  await cell.focus()
  await page.keyboard.press('F2')
  const editor = page.getByRole('dialog', { name: 'Edit payload', exact: true })
  await editor.getByRole('tab', { name: 'Code', exact: true }).click()
  await editor.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.insertText('{"id":9007199254740995,"changed":true}')
  await editor.getByRole('button', { name: 'Apply to draft' }).click()
  await page.getByRole('button', { name: 'Review 1 change(s)' }).click()
  const review = page.getByRole('dialog', { name: 'Review row changes', exact: true })
  await expect(review).toContainText('9007199254740995')
  await review.getByLabel('Reason').fill('change a profile')
  await review.getByRole('button', { name: 'Submit for approval', exact: true }).click()
  await expect(review.getByRole('alert')).toContainText('write conflict')
  await expect(
    review.getByRole('button', { name: 'Submit for approval', exact: true }),
  ).toBeDisabled()
  await review.getByRole('button', { name: 'Keep editing' }).click()
  await expect(page.getByRole('button', { name: 'Review 1 change(s)' })).toBeVisible()
})

test('PostgreSQL array inspection keeps raw syntax and precise JSON copies separate', async ({
  page,
}) => {
  await serveEditableTable(page)
  await mockClipboard(page)
  const raw = '[2:4]={9007199254740993,NULL,"NULL"}'
  await page.route('**/api/v1/query/execute', (route) =>
    route.fulfill({
      json: {
        columns: [{ name: 'payload', type: 'int8[]' }],
        rows: [[raw]],
        rowCount: 1,
        elapsedMs: 1,
        auditId: 'array-grid',
        truncated: false,
        limits: { maxCost: 50, maxRows: 1000 },
      },
    }),
  )
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await page.locator('.result tr[data-row] td').press('Enter')
  const viewer = page.getByRole('dialog', { name: 'payload', exact: true })
  await expect(viewer).toContainText('Array shown as JSON')
  await expect(viewer.getByRole('region', { name: 'JSON fields', exact: true })).toContainText(
    '9007199254740993',
  )
  await viewer.getByRole('button', { name: 'Copy original', exact: true }).click()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('test.clipboard'))).toBe(raw)
  await viewer.getByRole('button', { name: 'Copy as JSON', exact: true }).click()
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
    .toBe('[\n  "9007199254740993",\n  null,\n  "NULL"\n]')
})

test('NULL is an explicit draft choice and revert leaves the saved empty string intact', async ({
  page,
}) => {
  await serveEditableTable(page)
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').last().dblclick()
  const editor = page.getByRole('dialog', { name: 'Edit note', exact: true })
  await expect(editor.getByLabel('New value')).toHaveValue('')
  await editor.getByRole('checkbox', { name: 'Set to NULL' }).check()
  await expect(editor.getByLabel('New value')).toBeDisabled()
  await editor.getByRole('button', { name: 'Apply to draft' }).click()
  await page.getByRole('button', { name: 'Review 1 change(s)' }).click()
  const review = page.getByRole('dialog', { name: 'Review row changes', exact: true })
  await expect(review.getByRole('cell', { name: '""', exact: true })).toBeVisible()
  await expect(review.getByRole('cell', { name: 'NULL', exact: true })).toBeVisible()
  await expect(review.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled()
  await review.getByRole('button', { name: 'Revert this row', exact: true }).click()
  await review.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Review 1 change(s)' })).toHaveCount(0)
  await expect(page.locator('.result tr[data-row]').first().locator('td').last()).toContainText(
    'EMPTY',
  )
})

test('JSON URL and HTML values stay inert under inspection', async ({ page }) => {
  const records = await serveEditableTable(page)
  if (!records[0]) throw new Error('Fixture row missing')
  records[0][2] = '{"url":"https://example.com/","html":"<img src=x onerror=alert(1)>"}'
  await page.addInitScript(() => {
    window.open = () => {
      throw new Error('A cell must never open a URL')
    }
  })
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').nth(2).press('Enter')
  const viewer = page.getByRole('dialog', { name: 'payload', exact: true })
  const url = viewer
    .getByRole('region', { name: 'JSON fields', exact: true })
    .getByRole('button', { name: 'url: "https://example.com/"', exact: true })
  await url.click({ modifiers: ['ControlOrMeta'] })
  await url.click()
  await page.keyboard.press('ControlOrMeta+Enter')
  await expect(viewer.getByRole('region', { name: 'JSON fields', exact: true })).toContainText(
    '<img src=x onerror=alert(1)>',
  )
  await expect(viewer.locator('img')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('long draft values scroll while review actions remain reachable', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 720 })
  await serveEditableTable(page)
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').nth(1).dblclick()
  const editor = page.getByRole('dialog', { name: 'Edit username', exact: true })
  await editor.getByLabel('New value').fill('A long fixture value with spaces.\n'.repeat(80))
  await editor.getByRole('button', { name: 'Apply to draft' }).click()
  await page.getByRole('button', { name: 'Review 1 change(s)' }).click()
  const review = page.getByRole('dialog', { name: 'Review row changes', exact: true })
  await review.getByLabel('Reason').fill('Check a long draft before saving')
  const save = review.getByRole('button', { name: 'Save changes', exact: true })
  await expect(save).toBeEnabled()
  await expect(save).toBeInViewport()
  const value = review.locator('td pre').last()
  expect(await value.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
})
