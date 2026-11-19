import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { openEditableTable, originalJSON, serveEditableTable } from './visual/editing'

function firstRecord(records: (string | null)[][]): (string | null)[] {
  const record = records[0]
  if (!record) throw new Error('Missing fixture row')
  return record
}

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
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).fill('alice edited')
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).press('Enter')
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
  const template = page.waitForRequest((request) => request.url().endsWith('/writes/row-template'))
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await expect(review).toContainText('9007199254740993')
  await review.getByLabel('Reason').fill('correct a fixture name')
  expect((await template).postDataJSON()).toMatchObject({
    original: { id: '9007199254740993', username: 'alice', payload: originalJSON, note: '' },
    changes: { username: 'alice edited' },
  })
  await review.getByRole('button', { name: /^Save(?: \d+(?: ready)? rows?| changes)$/u }).click()
  await expect(review).toBeHidden()
  expect(writes).toHaveLength(1)
  expect(writes[0]).toMatchObject({ expectedAffectedRows: 1, reason: 'correct a fixture name' })
  await expect(page.getByRole('alert').filter({ hasText: '1 saved' })).toBeVisible()
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
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await expect(review).toContainText('9007199254740995')
  await review.getByLabel('Reason').fill('change a profile')
  await review
    .getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u })
    .click()
  await expect(review.getByRole('alert').filter({ hasText: 'write conflict' })).toBeVisible()
  await expect(
    review.getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u }),
  ).toHaveCount(0)
  await review.getByRole('button', { name: 'Close' }).click()
  await expect(page.getByRole('button', { name: /^Changes \(1\)/u })).toBeVisible()
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
  const editor = page.getByRole('textbox', { name: 'Edit note', exact: true })
  await expect(editor).toHaveValue('')
  await editor.press('Alt+Enter')
  await expect(editor).toHaveAttribute('placeholder', 'NULL')
  await editor.press('Enter')
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await expect(review.getByRole('cell', { name: '""', exact: true })).toBeVisible()
  await expect(review.getByRole('cell', { name: 'NULL', exact: true })).toBeVisible()
  await expect(
    review.getByRole('button', { name: /^Save(?: \d+(?: ready)? rows?| changes)$/u }),
  ).toBeDisabled()
  await review.getByRole('button', { name: /^More actions for/u }).click()
  await review.getByRole('button', { name: 'Discard row changes', exact: true }).click()
  await expect(review).toBeHidden()
  await expect(page.getByRole('button', { name: 'Changes', exact: true })).toBeDisabled()
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
  const editor = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await editor.fill('A long fixture value with spaces. '.repeat(80))
  await editor.press('Enter')
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await review.getByLabel('Reason').fill('Check a long draft before saving')
  const save = review.getByRole('button', { name: /^Save(?: \d+(?: ready)? rows?| changes)$/u })
  await expect(save).toBeEnabled()
  await expect(save).toBeInViewport()
  const value = review.locator('td pre').last()
  expect(await value.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
})

test('inline keyboard editing cancels, preserves IME text and commits with Tab without opening a dialog', async ({
  page,
}) => {
  await serveEditableTable(page)
  await openEditableTable(page)
  const cell = page.locator('.result tr[data-row]').first().locator('td').nth(1)
  await cell.press('F2')
  let input = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await input.fill('cancelled')
  await input.press('Escape')
  await expect(cell).toContainText('alice')
  await expect(cell).toBeFocused()
  await cell.press('F2')
  input = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await input.fill('正在組字')
  await input.dispatchEvent('compositionstart')
  await input.press('Enter')
  await expect(input).toBeVisible()
  await input.dispatchEvent('compositionend')
  await input.press('Tab')
  await expect(input).toHaveCount(0)
  await expect(cell).toContainText('正在組字')
  await expect(page.getByRole('dialog', { name: 'Edit payload', exact: true })).toHaveCount(0)
  await expect(page.locator('.result tr[data-row]').first().locator('td').nth(2)).toBeFocused()
})

test('conflicts compare three values and explicitly rebase before saving', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 650 })
  const records = await serveEditableTable(page)
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').nth(1).dblclick()
  const input = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await input.fill('my change')
  await input.press('Enter')
  firstRecord(records)[1] = 'someone else'
  await page.getByRole('button', { name: /^Changes \(1\)/ }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await expect(review).toContainText('Current database')
  for (const text of ['"alice"', '"someone else"', '"my change"'])
    await expect(review.getByRole('cell', { name: text, exact: true })).toBeVisible()
  await expect(review.getByLabel('Reason')).toHaveCount(0)
  await expect(
    review.getByRole('button', { name: /^Save(?: \d+(?: ready)? rows?| changes)$/u }),
  ).toHaveCount(0)
  await expect(review.getByRole('combobox', { name: 'Resolve username' })).toBeInViewport()
  expect(
    await review.locator('table').evaluate((element) => element.scrollWidth <= element.clientWidth),
  ).toBe(true)
  await review.getByRole('combobox', { name: 'Resolve username' }).click()
  await review.getByRole('option', { name: 'Keep mine', exact: true }).click()
  await review.getByRole('button', { name: 'Apply choices' }).click()
  await expect(review).not.toContainText('Current database')
  await review.getByLabel('Reason').fill('resolve a concurrent change')
  await expect(
    review.getByRole('button', { name: /^Save(?: \d+(?: ready)? rows?| changes)$/u }),
  ).toBeEnabled()
  await expect(review.getByRole('cell', { name: '"someone else"', exact: true })).toBeVisible()
})

test('fixed Changes entry works from SQL and returns to an exact row without losing the saved page', async ({
  page,
}) => {
  await serveEditableTable(page)
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').nth(1).dblclick()
  const input = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await input.fill('draft')
  await input.press('Enter')
  const region = page.getByRole('region', { name: 'Table data', exact: true })
  await region.getByRole('button', { name: 'Next', exact: true }).click()
  await expect(region).toContainText('member-50')
  await page.getByRole('tab', { name: 'SQL', exact: true }).click()
  await page.getByRole('button', { name: /^Changes \(1\)/ }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await review.getByRole('button', { name: 'Show row', exact: true }).click()
  await expect(region).toContainText('Showing requested row')
  await expect(region).toContainText('draft')
  await expect(region.getByRole('button', { name: 'Next', exact: true })).toBeDisabled()
  await expect(region.getByLabel('Rows', { exact: true })).toBeDisabled()
  await region.getByRole('button', { name: 'Return to saved page and filter' }).click()
  await expect(region).toContainText('member-50')
})

test('pending requests remain in the workspace and rejected requests restore their draft', async ({
  page,
}) => {
  await serveEditableTable(page, true)
  await page.route('**/api/v1/writes', (route) =>
    route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000002', state: 'pending' } }),
  )
  let state = 'pending'
  await page.route('**/api/v1/writes/00000000-0000-0000-0000-000000000002', (route) =>
    route.fulfill({
      json: { request: { state, decisionNote: state === 'rejected' ? 'Add more context' : '' } },
    }),
  )
  await openEditableTable(page)
  await page.locator('.result tr[data-row]').first().locator('td').nth(1).dblclick()
  const input = page.getByRole('textbox', { name: 'Edit username', exact: true })
  await input.fill('pending draft')
  await input.press('Enter')
  await page.getByRole('button', { name: /^Changes \(1\)/ }).click()
  const review = page.getByRole('dialog', { name: 'Changes', exact: true })
  await review.getByLabel('Reason').fill('please review')
  await review
    .getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u })
    .click()
  await expect(review).toBeHidden()
  await page.getByRole('button', { name: /^Changes.*pending/u }).click()
  await expect(review.getByRole('region', { name: 'Submitted requests' })).toContainText(
    'Awaiting approval',
  )
  await expect(review).not.toContainText('submitted for approval')
  state = 'rejected'
  await review.getByRole('button', { name: 'Refresh statuses', exact: true }).click()
  await expect(review).toContainText('pending draft')
  await expect(review).toContainText('Add more context')
  await expect(
    review.getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u }),
  ).toHaveCount(0)
  await review.getByRole('button', { name: 'Retry check', exact: true }).click()
  await expect(
    review.getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u }),
  ).toBeDisabled()
  await review.getByLabel('Reason').fill('more context for the retry')
  await expect(
    review.getByRole('button', { name: /^Submit(?: \d+(?: ready)? rows?)? for approval$/u }),
  ).toBeEnabled()
})

for (const field of [
  { type: 'numeric(30,12)', initial: '1.000000000001', next: '9007199254740993.000000000001' },
  { type: 'boolean', initial: 'true', next: 'false' },
  { type: 'date', initial: '2026-11-19', next: '2026-12-20' },
  {
    type: 'timestamp(6) with time zone',
    initial: '2026-11-19 12:00:00.123456+08:00',
    next: '2026-11-20 13:00:00.654321+08:00',
  },
  { type: 'public.state', initial: 'open', next: 'NULL', enumValues: ['open', '', 'NULL'] },
]) {
  test(`typed ${field.type} editor preserves its exact draft representation`, async ({ page }) => {
    const records = await serveEditableTable(page)
    firstRecord(records)[1] = field.initial
    await page.route('**/api/v1/schema/columns**', (route) =>
      route.fulfill({
        json: [
          { name: 'id', type: 'bigint', primaryKeyPosition: 1, nullable: false, denied: false },
          {
            name: 'username',
            type: field.type,
            nullable: true,
            denied: false,
            ...('enumValues' in field ? { enumValues: field.enumValues } : {}),
          },
          { name: 'payload', type: 'jsonb', nullable: false, denied: false },
          { name: 'note', type: 'text', nullable: true, denied: false },
        ],
      }),
    )
    await openEditableTable(page)
    await page.locator('.result tr[data-row]').first().locator('td').nth(1).dblclick()
    if (field.type.startsWith('numeric')) {
      const input = page.getByRole('textbox', { name: 'Edit username', exact: true })
      await input.fill(field.next)
      await input.press('Enter')
    } else {
      const editor = page.getByRole('dialog', { name: 'Edit username', exact: true })
      if (field.type === 'boolean' || 'enumValues' in field) {
        await editor.getByRole('combobox', { name: 'New value' }).click()
        await editor
          .getByRole('option', {
            name: field.type === 'boolean' ? field.next : JSON.stringify(field.next),
            exact: true,
          })
          .click()
      } else {
        const input = editor.getByLabel('New value')
        if (field.type.startsWith('timestamp')) {
          await input.fill('2026-11-20 13:00:00.654321')
          await editor.getByRole('button', { name: 'Apply to draft' }).click()
          await expect(editor.getByRole('alert')).toContainText('offset')
        }
        await input.fill(field.next)
      }
      await editor.getByRole('button', { name: 'Apply to draft' }).click()
    }
    await page.getByRole('button', { name: /^Changes \(1\)/ }).click()
    const review = page.getByRole('dialog', { name: 'Changes', exact: true })
    await expect(
      review.getByRole('cell', { name: JSON.stringify(field.next), exact: true }),
    ).toBeVisible()
  })
}

test('normal review has one entry and one primary action, with tools and history revealed only when needed', async ({
  page,
}) => {
  const records = await serveEditableTable(page)
  await page.route('**/api/v1/writes', async (route) => {
    firstRecord(records)[1] = 'saved name'
    await route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', state: 'applied' } })
  })
  await page.route('**/api/v1/writes/00000000-0000-0000-0000-000000000001', (route) =>
    route.fulfill({ json: { request: { state: 'applied' } } }),
  )
  await openEditableTable(page)
  await expect(page.getByRole('button', { name: /^Review \d/u })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Revert all', exact: true })).toHaveCount(0)
  await expect(page.getByText('Double-click or F2 to edit.', { exact: false })).toHaveCount(0)
  await page.getByRole('button', { name: 'Editing shortcuts', exact: true }).click()
  await expect(page.getByText('Double-click or F2 to edit.', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Editing shortcuts', exact: true }).click()
  const cell = page.locator('.result tr[data-row]').first().locator('td').nth(1)
  await cell.dblclick()
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).fill('saved name')
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).press('Enter')
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  const dialog = page.getByRole('dialog', { name: 'Changes', exact: true })
  await dialog.getByLabel('Reason').fill('Correct a name')
  await expect(dialog.locator('footer button')).toHaveCount(1)
  await expect(dialog.getByRole('button', { name: 'Keep editing', exact: true })).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: /Retry|Check again/u })).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: 'Copy draft', exact: true })).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: 'SQL', exact: true })).toHaveCount(0)
  await expect(dialog).not.toContainText('Each row is a separate guarded write')
  await expect(dialog).not.toContainText('Drafts stay in memory')
  await dialog.getByRole('button', { name: /^More actions for/u }).click()
  await dialog.getByRole('button', { name: 'SQL', exact: true }).click()
  await expect(dialog.locator('.cm-editor')).toHaveCount(1)
  await dialog.getByRole('button', { name: /^More actions for/u }).click()
  await expect(dialog.locator('.cm-editor')).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(cell).toContainText('saved name')
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  await expect(dialog.getByLabel('Reason')).toHaveValue('Correct a name')
  await dialog.getByRole('button', { name: 'Save 1 row', exact: true }).click()
  await expect(dialog).toBeHidden()
  await page.locator('.result tr[data-row]').nth(1).locator('td').nth(1).dblclick()
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).fill('second draft')
  await page.getByRole('textbox', { name: 'Edit username', exact: true }).press('Enter')
  await page.getByRole('button', { name: /^Changes \(1\)/u }).click()
  await expect(dialog.getByRole('button', { name: /^Submitted \(1\)/u })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await expect(dialog.getByRole('link', { name: 'View request', exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: /^Submitted \(1\)/u }).click()
  await expect(dialog.getByRole('link', { name: 'View request', exact: true })).toBeVisible()
})

test('partial review names ready rows and keeps deleted-row recovery visible', async ({ page }) => {
  const records = await serveEditableTable(page)
  const writes: { statement: string }[] = []
  await page.route('**/api/v1/writes', async (route) => {
    writes.push(route.request().postDataJSON() as { statement: string })
    await route.fulfill({ json: { id: '00000000-0000-0000-0000-000000000001', state: 'applied' } })
  })
  await openEditableTable(page)
  for (const [index, value] of ['deleted draft', 'ready draft'].entries()) {
    await page.locator('.result tr[data-row]').nth(index).locator('td').nth(1).dblclick()
    const editor = page.getByRole('textbox', { name: 'Edit username', exact: true })
    await editor.fill(value)
    await editor.press('Enter')
  }
  records.splice(0, 1)
  await page.getByRole('button', { name: /^Changes \(2\)/u }).click()
  const dialog = page.getByRole('dialog', { name: 'Changes', exact: true })
  await expect(dialog.getByRole('alert')).toContainText('deleted')
  await expect(dialog.getByRole('button', { name: 'Copy draft', exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Retry check', exact: true })).toHaveCount(0)
  await dialog.getByLabel('Reason').fill('Save the available row')
  await dialog.getByRole('button', { name: 'Save 1 ready row', exact: true }).click()
  expect(writes).toHaveLength(1)
  expect(writes[0]?.statement).toContain('ready draft')
  expect(writes[0]?.statement).not.toContain('deleted draft')
  await expect(dialog).toContainText('deleted draft')
  await expect(dialog.getByLabel('Reason')).toHaveCount(0)
})

test('long partial conflicts scroll only the dialog body and keep Close and Save visible', async ({
  page,
}) => {
  await page.setViewportSize({ width: 900, height: 650 })
  const records = await serveEditableTable(page)
  await openEditableTable(page)
  for (const index of [0, 1]) {
    await page.locator('.result tr[data-row]').nth(index).locator('td').nth(1).dblclick()
    const editor = page.getByRole('textbox', { name: 'Edit username', exact: true })
    await editor.fill('my edited name ' + index)
    await editor.press('Enter')
  }
  firstRecord(records)[1] = 'Remote long value with words. '.repeat(100)
  firstRecord(records)[3] = 'A remotely changed note.\n'.repeat(40)
  await page.getByRole('button', { name: /^Changes \(2\)/u }).click()
  const dialog = page.getByRole('dialog', { name: 'Changes', exact: true })
  await dialog.getByRole('combobox', { name: 'Resolve username' }).click()
  await dialog.getByRole('option', { name: 'Keep mine', exact: true }).click()
  await dialog.getByLabel('Reason').fill('Save the ready row')
  const close = dialog.getByRole('button', { name: 'Close', exact: true })
  const save = dialog.getByRole('button', { name: 'Save 1 ready row', exact: true })
  for (const control of [close, save]) {
    await expect(control).toBeInViewport()
    const inside = await control.evaluate((element) => {
      const box = element.getBoundingClientRect()
      const parent = element.closest('dialog')?.getBoundingClientRect()
      return parent && box.top >= parent.top && box.bottom <= parent.bottom
    })
    expect(inside).toBe(true)
  }
  expect(await dialog.evaluate((element) => element.scrollTop)).toBe(0)
  await close.click()
  await expect(dialog).toBeHidden()
  await expect(page.getByRole('button', { name: /^Changes \(2\)/u })).toBeVisible()
})
