import { expect, test, type Page } from '@playwright/test'

import type { components } from '@/gen/api'
import { serveFixedApi } from './visual/api'
import { answer, answerJson, calls, limits, record, verdict } from './redesign/fixtures'

type Schemas = components['schemas']

/*
 * FEAT-075. A partitioned table is listed once, its partitions are read where
 * the table is read, and the plan says which of them a statement scans.
 */

const rounds: Schemas['TableRef'] = {
  schema: 'public',
  name: 'rounds',
  kind: 'partitioned',
  partitions: 3,
}

/** A database long enough that its tree is taller than the sidebar. */
const manyTables: Schemas['TableRef'][] = [
  ...Array.from({ length: 120 }, (_, i) => ({
    schema: 'public',
    name: `ledger_${String(i).padStart(3, '0')}`,
    kind: 'table' as const,
  })),
  { schema: 'public', name: 'member', kind: 'table' },
  rounds,
]

const partitioning: Schemas['TablePartitioning'] = {
  schema: 'public',
  name: 'rounds',
  strategy: 'range',
  key: 'RANGE (id)',
  approxRows: 1250,
  totalBytes: 3 * 1024 * 1024,
  partitions: [
    {
      schema: 'public',
      name: 'rounds_2026_09',
      bound: "FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')",
      isDefault: false,
      partitioned: false,
      approxRows: 1200,
      totalBytes: 2 * 1024 * 1024,
    },
    {
      schema: 'public',
      name: 'rounds_2026_10',
      bound: "FOR VALUES FROM ('2026-10-01') TO ('2026-11-01')",
      isDefault: false,
      partitioned: false,
      approxRows: null,
      totalBytes: 8192,
    },
    {
      schema: 'public',
      name: 'rounds_default',
      bound: 'DEFAULT',
      isDefault: true,
      partitioned: false,
      approxRows: 50,
      totalBytes: 1024 * 1024,
    },
  ],
  warnings: [
    {
      code: 'default_has_rows',
      message: 'The DEFAULT partition holds rows.',
      subject: 'rounds_default',
    },
    {
      code: 'range_ends_soon',
      message: 'The highest partition ends at 2026-11-01 00:00 UTC.',
      subject: 'rounds_2026_10',
    },
  ],
}

async function pickTable(page: Page, name: string): Promise<void> {
  await page.getByRole('button', { name: /aviorfixture/u }).click()
  await page.locator('button.node').filter({ hasText: name }).first().click()
}

/** The column panel's controls sit inside the sidebar, with room to be pressed. */
async function expectColumnsReachable(page: Page): Promise<void> {
  const tree = page.locator('aside.tree')
  const columns = tree.locator('section.columns')
  const insert = columns.locator('button.insert')
  await expect(insert).toBeVisible()
  await expect(columns).toContainText(/\d+ of \d+ columns/u)
  const [treeBox, columnsBox, insertBox] = await Promise.all([
    tree.boundingBox(),
    columns.boundingBox(),
    insert.boundingBox(),
  ])
  expect(treeBox).not.toBeNull()
  expect(columnsBox?.height ?? 0).toBeGreaterThan((treeBox?.height ?? 0) * 0.4)
  expect((insertBox?.y ?? 0) + (insertBox?.height ?? 0)).toBeLessThanOrEqual(
    (treeBox?.y ?? 0) + (treeBox?.height ?? 0),
  )
}

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
  await answerJson(page, '/schema/tables', manyTables)
})

test('the columns and Insert stay reachable under a long table list', async ({ page }) => {
  await page.goto('/')
  await pickTable(page, 'member')
  await expectColumnsReachable(page)
  await expect(page.locator('section.columns .order-by')).toBeVisible()

  await page.goto('/writes/new')
  await pickTable(page, 'member')
  await expectColumnsReachable(page)
})

test('a partitioned table is listed once, with its partition count', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/u }).click()
  const node = page.locator('button.node').filter({ hasText: 'rounds' })
  await expect(node).toHaveCount(1)
  await expect(node.locator('.partitioned')).toHaveText('partitioned · 3')
  await expect(page.locator('button.node').filter({ hasText: 'rounds_2026' })).toHaveCount(0)
})

test('the table page reads a partitioned table as its partitions', async ({ page }) => {
  await answerJson(page, '/schema/partitions', partitioning)
  await answerJson(page, '/query/execute', {
    columns: [{ name: 'exact_rows', type: 'int8' }],
    rows: [['1234']],
    rowCount: 1,
    truncated: false,
    elapsedMs: 12,
    auditId: 'aud-75',
    limits,
  })
  const seen = record(page)
  await page.goto('/objects/table?database=aviorfixture&schema=public&name=rounds')

  const card = page.locator('section.partitions')
  await expect(card.locator('.summary')).toHaveText(
    'RANGE (id) · 3 partitions · ~1,250 rows (estimated) · 3.0 MB',
  )
  await expect(card.locator('.warning')).toHaveCount(2)
  await expect(card.locator('tbody tr')).toHaveCount(3)
  const row = (name: string) => card.locator('tbody tr').filter({ hasText: name })
  await expect(card.locator('tbody tr').first().locator('.bound')).toHaveText('DEFAULT')
  await expect(row('rounds_2026_10').locator('.estimate')).toHaveText('unknown')
  expect(calls(seen, 'POST', '/query/execute')).toHaveLength(0)

  await row('rounds_2026_09').getByRole('button', { name: 'Count' }).click()
  await expect(row('rounds_2026_09').locator('.counted')).toHaveText('1234')
  const [sent] = calls(seen, 'POST', '/query/execute')
  expect((sent?.body as { statement?: string } | undefined)?.statement).toBe(
    'SELECT count(ctid) AS exact_rows FROM "public"."rounds_2026_09"',
  )
})

test('the plan says which partitions a statement scans, out of how many', async ({ page }) => {
  await answer(page, '/query/inspect', (route) =>
    route.fulfill({
      json: verdict(12, {
        referencedTables: ['public.rounds'],
        partitionScans: [
          {
            schema: 'public',
            name: 'rounds',
            total: 3,
            scanned: [{ schema: 'public', name: 'rounds_2026_09', estimatedRows: 1200 }],
          },
        ],
      }),
    }),
  )
  await page.goto('/')
  await pickTable(page, 'rounds')
  await page.getByRole('button', { name: 'Inspect' }).click()

  const scans = page.locator('.partition-scans')
  await expect(scans.locator('.scan-summary')).toHaveText(
    /public\.rounds \(partitioned\) · scanned 1 \/\s*3 partitions/u,
  )
  await expect(scans.locator('li')).toHaveText(['rounds_2026_09~1,200 rows'])
})

const fallback: Schemas['PartitionEntry'] = {
  schema: 'public',
  name: 'rounds_default',
  bound: 'DEFAULT',
  isDefault: true,
  partitioned: false,
  approxRows: 0,
  totalBytes: 8192,
}

/** Twenty-five years of months, as the server orders them, and the DEFAULT last. */
function yearsOfMonths(count: number): Schemas['TablePartitioning'] {
  const months = Array.from({ length: count }, (_, i) => {
    const from = new Date(Date.UTC(2001, i, 1)).toISOString().slice(0, 10)
    const to = new Date(Date.UTC(2001, i + 1, 1)).toISOString().slice(0, 10)
    return {
      schema: 'public',
      name: `rounds_${from.slice(0, 7).replace('-', '_')}`,
      bound: `FOR VALUES FROM ('${from}') TO ('${to}')`,
      isDefault: false,
      partitioned: false,
      approxRows: 100,
      totalBytes: 8192,
    }
  })
  return {
    ...partitioning,
    partitions: [...months, fallback],
    warnings: [],
  }
}

test('hundreds of partitions show the newest and the DEFAULT, and the rest on request', async ({
  page,
}) => {
  await answerJson(page, '/schema/partitions', yearsOfMonths(300))
  await page.goto('/objects/table?database=aviorfixture&schema=public&name=rounds')

  const card = page.locator('section.partitions')
  const names = card.locator('tbody tr td.name')
  await expect(card.locator('.summary')).toContainText('301 partitions')
  await expect(names).toHaveCount(13)
  await expect(names.nth(0)).toHaveText('rounds_default')
  await expect(names.nth(1)).toHaveText('rounds_2025_12')
  await expect(names.nth(12)).toHaveText('rounds_2025_01')
  await expect(card.locator('.list-controls')).toContainText('288 more not shown')
  await page.screenshot({ path: test.info().outputPath('default.png') })

  await card.getByRole('button', { name: 'Show all 301 partitions' }).click()
  await expect(names).toHaveCount(50)
  await expect(card.locator('.pager')).toContainText('Page 1 of 7')
  await card.locator('.pager').getByRole('button', { name: 'Next' }).click()
  await expect(card.locator('.pager')).toContainText('Page 2 of 7')
  await expect(names.first()).toHaveText('rounds_2021_11')

  await card.getByLabel('Filter partitions').fill('rounds_2003')
  await expect(card.locator('.matched')).toHaveText('12 of 301')
  await expect(names).toHaveCount(12)
  await expect(names.first()).toHaveText('rounds_2003_12')
  await expect(card.locator('.pager')).toHaveCount(0)

  await card.locator('#partition-order').click()
  await page.locator('.option').filter({ hasText: 'Oldest first' }).click()
  await expect(names.first()).toHaveText('rounds_2003_01')
})
