import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { addressOf, nodes } from './visual/lineage'

/*
 * FEAT-074: nothing says it is empty while it is loading.
 *
 * Each case holds one request of the fixed API open, asserts the page says it
 * is waiting and does not say it is empty, then lets the answer through and
 * asserts the answer is drawn. A page that shows its empty text while its
 * request is pending fails here.
 */

test.use({ viewport: { width: 1440, height: 1000 }, timezoneId: 'UTC', locale: 'en-US' })

type Held = { release: () => void; asked: Promise<void> }

/** Holds every request to one API path until released, then answers it from the fixtures. */
async function hold(page: Page, path: string | RegExp, method = 'GET'): Promise<Held> {
  let release: () => void = () => undefined
  let asked: () => void = () => undefined
  const released = new Promise<void>((resolve) => (release = resolve))
  const seen = new Promise<void>((resolve) => (asked = resolve))
  const matches = (pathname: string) => {
    const bare = pathname.replace(/^\/api\/v1/u, '')
    return typeof path === 'string' ? bare === path : path.test(bare)
  }
  await page.route(
    (url) => url.pathname.startsWith('/api/v1/') && matches(url.pathname),
    async (route) => {
      if (route.request().method() !== method) {
        await route.fallback()
        return
      }
      asked()
      await released
      await route.fallback()
    },
  )
  return { release, asked: seen }
}

const loading = (page: Page, label: string) => page.getByRole('status', { name: label })

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
})

test('FEAT-074 AC-6: the schema tree shows it is loading, never its empty text, while the databases and tables are pending', async ({
  page,
}) => {
  const databases = await hold(page, '/schema/databases')
  await page.goto('/')
  await databases.asked

  await expect(loading(page, 'Loading databases')).toBeVisible()
  await expect(page.getByText(/Nothing matches/u)).toHaveCount(0)

  const tables = await hold(page, '/schema/tables')
  databases.release()
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  await tables.asked

  await expect(loading(page, 'Loading tables')).toBeVisible()
  await expect(page.getByText('No tables match.')).toHaveCount(0)

  tables.release()
  await expect(page.getByRole('button', { name: /^member\s+public/u })).toBeVisible()
  await expect(loading(page, 'Loading tables')).toHaveCount(0)
})

test('FEAT-074 AC-6: the query console shows it is loading, never its empty text, while the statement is running or being inspected', async ({
  page,
}) => {
  await page.goto('/')
  await page.getByRole('button', { name: /aviorfixture/ }).click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(page.locator('.cm-content')).toContainText('LIMIT 100')

  const execute = await hold(page, '/query/execute', 'POST')
  await page.getByRole('button', { name: 'Execute' }).click()
  await execute.asked

  await expect(loading(page, 'Running the statement')).toBeVisible()
  await expect(page.getByText('Rows appear here when a statement runs.')).toHaveCount(0)

  execute.release()
  await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await expect(loading(page, 'Running the statement')).toHaveCount(0)

  const inspect = await hold(page, '/query/inspect', 'POST')
  await page.getByRole('button', { name: 'Inspect' }).click()
  await inspect.asked

  await expect(loading(page, 'Inspecting the statement')).toBeVisible()
  await expect(page.getByText('Rows appear here when a statement runs.')).toHaveCount(0)
  await expect(page.getByText('Inspect a statement to read its plan.')).toHaveCount(0)

  inspect.release()
  await expect(page.getByText('This column is restricted.').first()).toBeVisible()
  await expect(loading(page, 'Inspecting the statement')).toHaveCount(0)
})

test('FEAT-074 AC-6: the object definition viewer shows it is loading, never its empty text, while a linked definition is pending', async ({
  page,
}) => {
  const definition = await hold(page, '/objects/definition')
  const query = new URLSearchParams({
    database: 'aviorfixture',
    schema: 'public',
    name: 'member_summary',
  })
  await page.goto(`/objects/view?${query.toString()}`)
  await definition.asked

  await expect(loading(page, 'Reading the definition')).toBeVisible()
  await expect(page.getByText(/to read its definition/u)).toHaveCount(0)

  definition.release()
  await expect(page.locator('.cm-content, pre').first()).toContainText('CREATE VIEW')
  await expect(loading(page, 'Reading the definition')).toHaveCount(0)
})

test('FEAT-074 AC-6: the table structure viewer shows it is loading, never its empty text, while a linked table is pending', async ({
  page,
}) => {
  const structure = await hold(page, '/objects/table')
  const query = new URLSearchParams({ database: 'aviorfixture', schema: 'public', name: 'member' })
  await page.goto(`/objects/table?${query.toString()}`)
  await structure.asked

  await expect(loading(page, 'Reading the table')).toBeVisible()
  await expect(page.getByText(/Pick a table to read its columns/u)).toHaveCount(0)

  structure.release()
  await expect(page.getByRole('heading', { name: /public\.member/ })).toBeVisible()
  await expect(loading(page, 'Reading the table')).toHaveCount(0)
})

test('FEAT-074 AC-6: the provision search shows it is loading, never its empty text, while the first role search is pending', async ({
  page,
}) => {
  await page.goto('/releases/provisioning')
  await expect(page.getByText('Name a database to see the roles that reach it.')).toBeVisible()

  const roles = await hold(page, '/provision/roles')
  await page.locator('#roles-database').fill('aviorfixture')
  await page.getByRole('button', { name: 'Search' }).click()
  await roles.asked

  await expect(loading(page, 'Searching the roles')).toBeVisible()
  await expect(page.getByText('Name a database to see the roles that reach it.')).toHaveCount(0)

  roles.release()
  await expect(page.getByText('svc_reporting').first()).toBeVisible()
  await expect(loading(page, 'Searching the roles')).toHaveCount(0)
})

test('FEAT-074 AC-6: the write request detail shows it is loading, never its empty text, while the request is pending', async ({
  page,
}) => {
  await page.goto('/writes')
  const detail = await hold(page, /^\/writes\/wr-[^/]+$/u)
  await page.locator('[data-request]').first().click()
  await detail.asked

  const reader = page.getByTestId('request-detail')
  await expect(reader.getByRole('status', { name: 'Opening the request' })).toBeVisible()
  await expect(page.getByText('Choose a request to read it.')).toHaveCount(0)

  detail.release()
  await expect(reader.getByText('Approval steps')).toBeVisible()
  await expect(reader.getByRole('status', { name: 'Opening the request' })).toHaveCount(0)
})

function lineageAt(extra: Record<string, string>): string {
  return `/lineage?${new URLSearchParams({ database: 'aviorfixture', ...extra }).toString()}`
}

test('FEAT-074 AC-4: the lineage canvas shows it is loading, never its empty text, while its first answer is pending', async ({
  page,
}) => {
  const graph = await hold(page, '/lineage/graph')
  await page.goto(lineageAt(addressOf(nodes.bet)))
  await graph.asked

  await expect(loading(page, 'Reading the lineage')).toBeVisible()
  await expect(page.getByText(/Pick a database and an object/u)).toHaveCount(0)

  graph.release()
  await expect(page.getByTestId('lineage-chart').locator('[data-node]').first()).toBeVisible()
  await expect(loading(page, 'Reading the lineage')).toHaveCount(0)
})

test('FEAT-074 AC-4: the lineage overview shows it is loading, never its empty text, while the overview is pending', async ({
  page,
}) => {
  const overview = await hold(page, '/lineage/overview')
  await page.goto(lineageAt({ mode: 'overview' }))
  await overview.asked

  await expect(loading(page, 'Reading the overview')).toBeVisible()
  await expect(page.getByText(/Pick a database to see its schemas/u)).toHaveCount(0)

  overview.release()
  await expect(page.getByTestId('lineage-galaxy')).toBeVisible()
  await expect(loading(page, 'Reading the overview')).toHaveCount(0)
})

test('FEAT-074 AC-7: the lineage toolbar shows it is loading, never its empty text, while its options are pending', async ({
  page,
}) => {
  const databases = await hold(page, '/schema/databases')
  const tables = await hold(page, '/schema/tables')
  await page.goto(lineageAt(addressOf(nodes.bet)))
  await databases.asked
  await tables.asked

  await expect(loading(page, 'Loading databases')).toBeVisible()
  await expect(loading(page, 'Loading objects')).toBeVisible()
  await expect(page.locator('#lineage-database')).toHaveCount(0)
  await expect(page.locator('#lineage-focus')).toHaveCount(0)

  databases.release()
  tables.release()
  await expect(page.locator('#lineage-database')).toBeVisible()
  await expect(page.locator('#lineage-focus')).toBeVisible()
  await expect(loading(page, 'Loading databases')).toHaveCount(0)
  await expect(loading(page, 'Loading objects')).toHaveCount(0)
})

test('FEAT-074 AC-2: the loader comes in its inline and section sizes, each a named status, the inline one with its words', async ({
  page,
}) => {
  const orb = async (label: string) => {
    const box = await loading(page, label).locator('.loader-orb').boundingBox()
    if (box === null) throw new Error(`the ${label} loader has no orb`)
    return box
  }
  const databases = await hold(page, '/schema/databases')
  await page.goto('/')
  await databases.asked
  const inline = loading(page, 'Loading databases')
  await expect(inline).toHaveAttribute('data-size', 'inline')
  await expect(inline).toContainText('Loading databases')
  const small = await orb('Loading databases')
  expect(small.height).toBeGreaterThanOrEqual(20)
  expect(small.height).toBeLessThanOrEqual(36)
  databases.release()

  const graph = await hold(page, '/lineage/graph')
  await page.goto(lineageAt(addressOf(nodes.bet)))
  await graph.asked
  const section = loading(page, 'Reading the lineage')
  await expect(section).toHaveAttribute('data-size', 'section')
  await expect(section).not.toContainText('Reading the lineage')
  const middle = await orb('Reading the lineage')
  expect(middle.height).toBeGreaterThanOrEqual(52)
  expect(middle.height).toBeLessThanOrEqual(76)
  graph.release()
})
