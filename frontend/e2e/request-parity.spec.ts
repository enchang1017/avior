import { readFileSync, writeFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import type { Page, Request, Route } from '@playwright/test'

import { administrator, get, serveFixedApi } from './visual/api'

/*
 * FEAT-065 AC-15: nothing the console sends has changed.
 *
 * One scripted session crosses every page that sends something that matters —
 * sign in, Run, Inspect, compose, withdraw and approve a write, decide a
 * release, read both audit logs, grant and revoke a database — and records
 * every request it makes: method, path, query and body. The recording beside
 * this file was taken by this same script against the console at 53c30bf,
 * before any of the redesign. A redesign that dropped a confirmation, sent a
 * field differently or skipped a check sends a different sequence, and fails.
 *
 * Order is compared step by step. Within a step, requests the page fires
 * together are compared as a set, because their order is the network's, not
 * the page's. The endpoints FEAT-066 to FEAT-069 add are the one difference
 * allowed, and they are listed rather than silently ignored.
 *
 * RECORD=path writes the recording instead of comparing against it.
 */

const recording = new URL('./request-parity/53c30bf.json', import.meta.url)

const introduced = [
  /^GET \/search(\?|$)/u,
  /^GET \/schema\/graph(\?|$)/u,
  /^GET \/audit\/histogram(\?|$)/u,
  /^GET \/audit\/mine\/histogram(\?|$)/u,
]

type Sent = string
type Step = { name: string; sent: Sent[] }

function describe(request: Request): Sent | null {
  const url = new URL(request.url())
  if (!url.pathname.startsWith('/api/v1/')) return null
  const query = [...url.searchParams.entries()].sort().map(([k, v]) => `${k}=${v}`)
  const path = url.pathname.slice('/api/v1'.length) + (query.length ? `?${query.join('&')}` : '')
  const raw = request.postData()
  const body = raw === null ? '' : ` ${JSON.stringify(sortKeys(JSON.parse(raw) as unknown))}`
  return `${request.method()} ${path}${body}`
}

function sortKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortKeys)
  if (value === null || typeof value !== 'object') return value
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, inner]) => [key, sortKeys(inner)]),
  )
}

const own = {
  id: 'wr-own-0001',
  database: 'aviorfixture',
  statement: "UPDATE public.member SET tier = 'gold' WHERE id = 42",
  kind: 'update',
  targetTable: 'public.member',
  reason: 'Parity: a request of my own',
  requestedBy: administrator.username,
  requestedAt: '2026-09-20T08:30:00Z',
  estimatedRows: 1,
  state: 'pending',
  reversible: true,
}

const theirs = { ...own, id: 'wr-theirs-0002', requestedBy: 'chen.yu', reason: 'Theirs' }

/** The answers the fixed API does not give, for the flows only this script walks. */
async function serveTheSession(page: Page): Promise<void> {
  let signedIn = false
  const json = (route: Route, body: unknown, status = 200) => route.fulfill({ status, json: body })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api\/v1/u, '')
    const method = route.request().method()
    if (path === '/auth/me' && !signedIn) return json(route, { code: 'auth.required' }, 401)
    if (path === '/auth/login' && method === 'POST') {
      signedIn = true
      return json(route, { user: administrator })
    }
    if (path === '/writes/inspect') {
      return json(route, { decision: 'allow', reasons: [], estimatedRows: 1, kind: 'update' })
    }
    if (path === '/writes/template') return json(route, { statement: own.statement })
    if (path === '/writes' && method === 'POST') return json(route, own, 201)
    if (path === '/writes' && method === 'GET') return json(route, [own, theirs])
    if (path === `/writes/${own.id}`) return json(route, { request: own, history: [] })
    if (path === `/writes/${theirs.id}`) return json(route, { request: theirs, history: [] })
    if (/^\/writes\/[^/]+\/(decision|withdrawal)$/u.test(path)) {
      return json(route, { request: { ...own, state: 'withdrawn' }, history: [] })
    }
    const decided = /^(\/releases\/[^/]+)\/decision$/u.exec(path)
    if (decided !== null) return json(route, get(decided[1] ?? '')?.body ?? {})
    if (method === 'PUT' && path.startsWith('/databases/')) {
      return json(route, get('/databases/grants')?.body ?? {})
    }
    return route.fallback()
  })
}

async function step(page: Page, steps: Step[], name: string, act: () => Promise<void>) {
  const sent: Sent[] = []
  const listen = (request: Request) => {
    const line = describe(request)
    if (line !== null) sent.push(line)
  }
  page.on('request', listen)
  await act()
  await page.waitForLoadState('networkidle')
  page.off('request', listen)
  steps.push({ name, sent: [...sent].sort() })
}

/*
 * A decision reloads the list once it has answered. The step waits for that
 * reload rather than for the network to go quiet: a page that already went
 * quiet once answers that question at once, and the reload would be counted
 * against whichever step came next.
 */
function listReloaded(page: Page): Promise<unknown> {
  return page.waitForResponse(
    (response) =>
      response.request().method() === 'GET' &&
      new URL(response.url()).pathname === '/api/v1/writes',
  )
}

async function pickTable(page: Page): Promise<void> {
  await page
    .getByRole('button', { name: /aviorfixture/ })
    .first()
    .click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(page.locator('.cm-content').first()).toContainText('public.member')
}

async function session(page: Page): Promise<Step[]> {
  const steps: Step[] = []
  await step(page, steps, 'sign in', async () => {
    await page.goto('/login')
    await page.getByLabel('Username').fill('developer')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL(/\/$/u)
  })
  await step(page, steps, 'pick a table', () => pickTable(page))
  await step(page, steps, 'run', async () => {
    await page.getByRole('button', { name: 'Execute' }).click()
    await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  })
  await step(page, steps, 'inspect', async () => {
    await page.getByRole('button', { name: 'Inspect' }).click()
    await expect(page.locator('section.verdict')).toBeVisible()
  })
  await step(page, steps, 'compose a write', async () => {
    await page.goto('/writes/new')
    await pickTable(page)
    await page.getByRole('button', { name: 'Check' }).click()
    await page.locator('#write-reason').fill(own.reason)
    await page.getByRole('button', { name: 'Ask for approval' }).click()
    await expect(page).toHaveURL(/\/writes$/u)
  })
  await step(page, steps, 'withdraw mine', async () => {
    await page.locator('[data-request]').nth(0).click()
    const reloaded = listReloaded(page)
    await page.getByRole('button', { name: 'Withdraw' }).click()
    await reloaded
  })
  await step(page, steps, 'approve theirs', async () => {
    await page.locator('[data-request]').nth(1).click()
    const reloaded = listReloaded(page)
    await page.getByRole('button', { name: 'Approve and run' }).click()
    await reloaded
  })
  await step(page, steps, 'decide a release', async () => {
    await page.goto('/releases/rel-0001-aaaa-bbbb')
    await page.getByRole('button', { name: 'Approve', exact: true }).click()
  })
  await step(page, steps, 'read my audit log', async () => {
    await page.goto('/audit')
  })
  await step(page, steps, 'read everyone’s audit log', async () => {
    await page.goto('/audit/all')
  })
  await step(page, steps, 'grant a database', async () => {
    await page.goto('/admin/databases')
    await page.getByRole('button', { name: 'Grant' }).first().click()
  })
  await step(page, steps, 'revoke it', async () => {
    await page.getByRole('button', { name: 'Reaches' }).first().click()
  })
  return steps
}

function withoutIntroduced(steps: Step[]): { steps: Step[]; added: Sent[] } {
  const added: Sent[] = []
  const kept = steps.map((one) => ({
    name: one.name,
    sent: one.sent.filter((line) => {
      const isNew = introduced.some((pattern) => pattern.test(line))
      if (isNew) added.push(line)
      return !isNew
    }),
  }))
  return { steps: kept, added }
}

test.use({ timezoneId: 'UTC', locale: 'en-US' })

test('a scripted session sends the requests it sent before the redesign', async ({ page }) => {
  await serveFixedApi(page, true)
  await serveTheSession(page)
  await page.clock.setFixedTime(new Date('2026-09-20T10:00:00Z'))
  await page.addInitScript(() => window.localStorage.setItem('avior.theme', 'dark'))

  const steps = await session(page)
  const record = process.env['RECORD']
  if (record !== undefined && record !== '') {
    writeFileSync(record, `${JSON.stringify(steps, null, 2)}\n`)
    return
  }

  const before = JSON.parse(readFileSync(recording, 'utf8')) as Step[]
  const now = withoutIntroduced(steps)
  expect(now.steps, 'the requests each step sends').toEqual(before)
  console.warn(`request parity: ${now.added.length} requests from FEAT-066 to FEAT-069`)
})
