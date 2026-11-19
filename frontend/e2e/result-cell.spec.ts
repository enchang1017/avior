import { expect, test } from '@playwright/test'

import { answerJson } from './redesign/fixtures'
import { serveFixedApi } from './visual/api'

const long = 'A long value '.repeat(20) + 'find-this-tail'
const values = [
  'short',
  long,
  null,
  '',
  ' \t\n',
  false,
  0,
  { id: [1, null] },
  'first\r\nsecond\tcolumn',
  '9007199254740993',
  '<script>alert(1)</script>',
]

test('cells focus quietly, expand deliberately and copy exact values', async ({ page }) => {
  await serveFixedApi(page)
  await answerJson(page, '/query/execute', {
    columns: [
      { name: 'id', type: 'bigint' },
      { name: 'value', type: 'text' },
      { name: 'secret', type: 'text', masked: true },
    ],
    rows: values.map((value, index) => [index + 1, value, 'never-expose-this']),
    rowCount: values.length,
    truncated: false,
    elapsedMs: 1,
    auditId: 'cell-test',
    limits: { maxCost: 50, maxRows: 1000 },
  })
  await page.addInitScript(() =>
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (value: string) => {
          localStorage.setItem('test.clipboard', value)
          return Promise.resolve()
        },
      },
    }),
  )
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  const execution = page.waitForRequest((request) => request.url().endsWith('/query/execute'))
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  expect((await execution).headers()['x-avior-exact-integers']).toBe('true')
  const rows = page.locator('.result tbody tr[data-row]')
  const cell = (index: number) => rows.nth(index).locator('td').nth(1)
  await cell(0).click()
  await expect(cell(0)).toBeFocused()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(cell(0).getByRole('button')).toHaveCount(0)
  await page.keyboard.press('Control+c')
  await expect.poll(() => page.evaluate(() => localStorage.getItem('test.clipboard'))).toBe('short')
  await cell(1).getByRole('button', { name: 'Inspect value', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'value', exact: true })
  await expect(dialog.locator('pre')).toHaveText(long)
  await dialog.getByRole('button', { name: 'Copy value' }).click()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('test.clipboard'))).toBe(long)
  await page.keyboard.press('Escape')
  await expect(cell(1).getByRole('button')).toBeFocused()
  await expect(cell(2)).toHaveAccessibleName('NULL')
  await expect(cell(3)).toHaveAccessibleName('Empty string')
  await expect(cell(4)).toHaveAccessibleName('Whitespace')
  await expect(page.locator('.result')).not.toContainText('never-expose-this')
  for (const index of [2, 0]) {
    const target = index === 2 ? cell(2) : rows.first().locator('td').nth(2)
    await target.click()
    await page.keyboard.press('Control+c')
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toBeHidden()
    await expect.poll(() => page.evaluate(() => localStorage.getItem('test.clipboard'))).toBe(long)
  }
  for (const [index, copied] of [
    [3, ''],
    [4, ' \t\n'],
    [5, 'false'],
    [6, '0'],
    [7, '{"id":[1,null]}'],
    [8, 'first\r\nsecond\tcolumn'],
    [9, '9007199254740993'],
  ] as const) {
    await cell(index).click()
    await page.keyboard.press('Meta+c')
    await expect
      .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
      .toBe(copied)
  }
  await expect(cell(10)).toHaveText('<script>alert(1)</script>')
  await expect(cell(10).locator('script')).toHaveCount(0)
  await page.locator('.result input[type="search"]').fill('find-this-tail')
  await expect(rows).toHaveCount(1)
  await expect(
    rows.first().getByRole('button', { name: 'Inspect value', exact: true }),
  ).toBeVisible()
})

test('a PostgreSQL bigint reaches the browser and clipboard without rounding', async ({ page }) => {
  await page.addInitScript(() =>
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (value: string) => {
          localStorage.setItem('test.clipboard', value)
          return Promise.resolve()
        },
      },
    }),
  )
  await page.goto('/login')
  await page.getByLabel('Username').fill('developer')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.locator('.node.database', { hasText: 'aviorfixture' }).click()
  await page
    .locator('.cm-content')
    .fill('SELECT 9223372036854775807::bigint AS large_id FROM public.member LIMIT 1')
  await page.getByRole('button', { name: 'Execute', exact: false }).click()
  const cell = page
    .locator('.result')
    .getByRole('cell', { name: '9223372036854775807', exact: true })
  await expect(cell).toBeVisible()
  await cell.click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.keyboard.press('Control+c')
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('test.clipboard')))
    .toBe('9223372036854775807')
})
