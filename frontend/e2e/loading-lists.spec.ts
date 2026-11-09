import { expect, test, type Page } from '@playwright/test'

import { observe } from './perf/measure'
import { serveFixedApi } from './visual/api'
import { serveObservatory } from './visual/observatory'

/*
 * FEAT-074: a list waiting for its rows shows rows, not an empty table.
 *
 * Each case holds the request a list's rows come from, asserts the page shows
 * its table-shaped skeleton and not its empty text, then lets the answer
 * through and asserts the rows replaced the skeleton.
 */

test.use({ viewport: { width: 1440, height: 1000 }, timezoneId: 'UTC', locale: 'en-US' })

type Held = { release: () => void; asked: Promise<void> }

/** Holds every GET to one API path until released, then answers it from the fixtures. */
async function hold(page: Page, path: string | RegExp): Promise<Held> {
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
      if (route.request().method() !== 'GET') {
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

const skeleton = (page: Page, label: string) => page.getByRole('status', { name: label })

type List = {
  page: string
  url: string
  path: string | RegExp
  label: string
  empty: RegExp
  observatory?: boolean
  /** What stands where the skeleton was, when the rows are not a table. */
  rows?: string
}

const lists: List[] = [
  {
    page: 'the audit log',
    url: '/audit',
    path: '/audit/mine',
    label: 'Reading the audit log',
    empty: /No submission in this range/u,
  },
  {
    page: 'write requests',
    url: '/writes',
    path: '/writes',
    label: 'Reading write requests',
    empty: /Nobody has asked for a row change|Nothing here matches/u,
    rows: '[data-request]',
  },
  {
    page: 'write captures',
    url: '/writes/captures',
    path: '/writes/captures',
    label: 'Reading backups',
    empty: /The write path has kept nothing yet|No backup is older/u,
  },
  {
    page: 'operators',
    url: '/admin/accounts',
    path: '/operators',
    label: 'Reading operators',
    empty: /No operators\.|No operator matches/u,
  },
  {
    page: 'service accounts',
    url: '/admin/service-accounts',
    path: '/service-accounts',
    label: 'Reading service accounts',
    empty: /No service accounts yet/u,
  },
  {
    page: 'database access',
    url: '/admin/databases',
    path: /^\/databases\/(grants|governed)$/u,
    label: 'Reading database access',
    empty: /No governed databases/u,
  },
  {
    page: 'column restrictions',
    url: '/admin/columns',
    path: '/columns/restrictions',
    label: 'Reading column restrictions',
    empty: /No column is restricted/u,
  },
  {
    page: 'tokens',
    url: '/account/tokens',
    path: '/tokens',
    label: 'Reading your tokens',
    empty: /None yet/u,
  },
  {
    page: "the observatory's Usage tab",
    url: '/observatory?tab=usage',
    path: '/observatory/usage',
    label: 'Reading usage',
    empty: /No statement was submitted in this window/u,
    observatory: true,
  },
  {
    page: "the observatory's Capacity tab",
    url: '/observatory?tab=capacity',
    path: '/observatory/databases',
    label: 'Reading capacity',
    empty: /No database you can reach has been measured yet/u,
    observatory: true,
  },
  {
    page: "the observatory's Last used tab",
    url: '/observatory?tab=last-used',
    path: '/observatory/objects',
    label: 'Reading when each object was last used',
    empty: /No object to show|Nothing here is flagged/u,
    observatory: true,
  },
  {
    page: "the observatory's Performance tab",
    url: '/observatory?tab=performance',
    path: '/observatory/statements',
    label: 'Reading top statements',
    empty: /No statement has been recorded/u,
    observatory: true,
  },
  {
    page: "the observatory's Routine timing tab",
    url: '/observatory?tab=routines',
    path: '/observatory/routines',
    label: 'Reading routine timing',
    empty: /No routine to show|No routine matches/u,
    observatory: true,
  },
]

for (const list of lists) {
  test(`FEAT-074 AC-7: ${list.page} shows skeleton rows, not an empty table, while its rows load`, async ({
    page,
  }) => {
    const missed = await serveFixedApi(page)
    if (list.observatory === true) await serveObservatory(page)
    const rows = await hold(page, list.path)
    await page.goto(list.url)
    await rows.asked

    const waiting = skeleton(page, list.label)
    await expect(waiting).toBeVisible()
    await expect(waiting.locator('[data-skeleton-row]').first()).toBeVisible()
    await expect(page.getByText(list.empty)).toHaveCount(0)

    rows.release()
    await expect(waiting).toHaveCount(0)
    const filled = list.rows === undefined ? page.getByRole('table') : page.locator(list.rows)
    await expect(filled.first()).toBeVisible()
    await expect(page.getByText(list.empty)).toHaveCount(0)
    expect(missed).toEqual([])
  })
}

test('FEAT-074 AC-7: the Performance tab shows skeleton rows for running now and table health while they load', async ({
  page,
}) => {
  await serveFixedApi(page)
  await serveObservatory(page)
  const activity = await hold(page, '/observatory/activity')
  const health = await hold(page, '/observatory/health')
  await page.goto('/observatory?tab=performance')
  await activity.asked
  await health.asked

  await expect(skeleton(page, 'Reading what is running now')).toBeVisible()
  await expect(skeleton(page, 'Reading table health')).toBeVisible()
  await expect(page.getByText(/Nothing has been running that long|No table to show/u)).toHaveCount(
    0,
  )

  activity.release()
  health.release()
  await expect(skeleton(page, 'Reading what is running now')).toHaveCount(0)
  await expect(skeleton(page, 'Reading table health')).toHaveCount(0)
})

test('FEAT-074 AC-8: a release opened from the queue shows its skeleton, never a blank page, while it loads', async ({
  page,
}) => {
  const missed = await serveFixedApi(page)
  await page.goto('/releases')
  const detail = await hold(page, /^\/releases\/rel-/u)
  await page.getByRole('link', { name: 'Review' }).first().click()
  await detail.asked

  await expect(page).toHaveURL(/\/releases\/rel-/u)
  await expect(skeleton(page, 'Reading the release')).toBeVisible()

  detail.release()
  await expect(skeleton(page, 'Reading the release')).toHaveCount(0)
  await expect(page.getByText('Approval steps')).toBeVisible()
  expect(missed).toEqual([])
})

test('FEAT-074 AC-11: the operators list replaces its skeleton with its rows without shifting the layout', async ({
  page,
}) => {
  await observe(page)
  await serveFixedApi(page)
  const rows = await hold(page, '/operators')
  await page.goto('/admin/accounts')
  await rows.asked
  await expect(skeleton(page, 'Reading operators')).toBeVisible()
  await page.waitForTimeout(300)
  const before = await page.evaluate(() => window.__perf.cls)

  rows.release()
  await expect(skeleton(page, 'Reading operators')).toHaveCount(0)
  await expect(page.getByRole('table').first()).toBeVisible()
  await page.waitForTimeout(500)
  const after = await page.evaluate(() => window.__perf.cls)

  expect(after - before, 'CLS from the skeleton to the rows').toBeLessThanOrEqual(0.02)
})
