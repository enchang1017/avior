import { expect, request, test } from '@playwright/test'
import type { APIRequestContext, Page } from '@playwright/test'

/*
 * FEAT-076. The database layer on the Provisioning page, across the browser,
 * the Go service and the governed server: a declared role that cannot connect
 * is granted CONNECT, a database is prepared for writes, and the roles list
 * says which roles cannot reach the database it is about.
 *
 * Each case provisions a database of its own through the API first. Only a
 * database Avior created has an owner role the provisioning credential can act
 * as, and a case leaning on another's database would pass or fail by order.
 */

async function apiAs(baseURL: string, username: string): Promise<APIRequestContext> {
  const api = await request.newContext({ baseURL })
  const login = await api.post('/api/v1/auth/login', { data: { username } })
  expect(login.ok(), `sign in as ${username}: ${login.status()}`).toBe(true)
  return api
}

function unique(prefix: string): string {
  return `${prefix}_${Math.random().toString(36).slice(2, 10)}`
}

async function provision(api: APIRequestContext, database: string, role: string): Promise<void> {
  const created = await api.post('/api/v1/provision/databases', { data: { name: database } })
  expect(created.ok(), `provision ${database}: ${created.status()}`).toBe(true)
  const group = await api.post('/api/v1/provision/roles', { data: { name: role } })
  expect(group.ok(), `create ${role}: ${group.status()}`).toBe(true)
}

async function openProvisioning(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill('dba')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.locator('.rail')).toBeVisible()
  await page.goto('/releases/provisioning')
}

test('FEAT-076 AC-13: the page grants connect and prepares a database for writes', async ({
  page,
  baseURL,
}) => {
  const database = unique('e2edb')
  const role = unique('role_e2e')
  await provision(await apiAs(baseURL ?? '', 'dba'), database, role)

  await openProvisioning(page)
  await page.locator('#provision-database').fill(database)
  await page.locator('#provision-roles').fill(role)
  await page.getByRole('button', { name: 'Check' }).click()

  const roleRow = page.getByRole('row', { name: new RegExp(`^${role}`) })
  await expect(roleRow).toContainText('cannot connect')
  await roleRow.getByRole('button', { name: 'Grant connect' }).click()
  await expect(roleRow).toContainText('may connect')
  await expect(roleRow.getByRole('button', { name: 'Grant connect' })).toHaveCount(0)

  const writeRow = page.getByRole('row', { name: /^Write path/ })
  await expect(writeRow).toContainText('not prepared')
  await expect(writeRow).toContainText('the capture table')
  await writeRow.getByRole('button', { name: 'Prepare for writes' }).click()
  await expect(writeRow).toContainText('prepared')
  await expect(writeRow).not.toContainText('not prepared')
  await expect(writeRow.getByRole('button', { name: 'Prepare for writes' })).toHaveCount(0)
})

/*
 * A database the way a real one arrives: provisioned, its group role created,
 * and a first release from CI that creates a table and grants the group SELECT
 * on it. The release is approved by the seeded approver and applied by Avior,
 * so the group holds a privilege in the database and, until somebody grants
 * it, cannot connect.
 */
async function releasedWithGrant(baseURL: string, database: string, group: string): Promise<void> {
  const dba = await apiAs(baseURL, 'dba')
  await provision(dba, database, group)
  const ci = await request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: 'Bearer rel_local_development_release_token' },
  })
  const submitted = await ci.post('/api/v1/releases', {
    data: {
      database,
      commitSha: Array.from({ length: 40 }, () => Math.floor(Math.random() * 16).toString(16)).join(
        '',
      ),
      author: `ci@${database}`,
      migrations: [
        {
          filename: '001_baseline.sql',
          sql: `CREATE TABLE public.round (id integer PRIMARY KEY);\nGRANT SELECT ON public.round TO ${group};`,
        },
      ],
      expectedSchemaDigest: 'avior:empty-schema',
    },
  })
  expect(submitted.status(), `submit: ${await submitted.text()}`).toBe(201)
  const { id } = (await submitted.json()) as { id: string }
  const approver = await apiAs(baseURL, 'approver')
  const decided = await approver.post(`/api/v1/releases/${id}/decision`, {
    data: { decision: 'approve', note: 'e2e' },
  })
  expect(decided.ok(), `approve: ${await decided.text()}`).toBe(true)
  const applied = await approver.post(`/api/v1/releases/${id}/apply`)
  expect(applied.ok(), `apply: ${await applied.text()}`).toBe(true)
}

async function searchRoles(page: Page, database: string): Promise<void> {
  await openProvisioning(page)
  await page.locator('#roles-database').fill(database)
  await page.getByRole('button', { name: 'Search' }).click()
  await expect(page.getByTestId('roles-map')).toBeVisible()
}

test('FEAT-076 AC-17: the roles map draws the database, its roles and their links', async ({
  page,
  baseURL,
}) => {
  const database = unique('e2edb')
  const group = unique('role_e2e')
  await releasedWithGrant(baseURL ?? '', database, group)
  const dba = await apiAs(baseURL ?? '', 'dba')
  // Named like the database and related to it in no way: AC-15 leaves it out.
  const lookalike = await dba.post('/api/v1/provision/roles', {
    data: { name: `${database}_demo` },
  })
  expect(lookalike.ok()).toBe(true)
  // A second group that may connect, and a login in both: drawn once, joined to each.
  const second = unique('role_e2e')
  const login = unique('e2e_app')
  expect((await dba.post('/api/v1/provision/roles', { data: { name: second } })).ok()).toBe(true)
  expect(
    (
      await dba.post(`/api/v1/provision/databases/${database}/connect`, { data: { role: second } })
    ).ok(),
  ).toBe(true)
  expect(
    (
      await dba.post('/api/v1/provision/logins', {
        data: { name: login, memberOf: [group, second] },
      })
    ).ok(),
  ).toBe(true)

  await searchRoles(page, database)

  const map = page.getByTestId('roles-map')
  await expect(map.getByTestId('roles-map-database')).toContainText(database)
  const owner = map.locator(`[data-role="${database}_schema_owner"]`)
  await expect(owner).toContainText('owner')
  await expect(owner).toContainText('owns 1 table')
  const blocked = map.locator(`[data-role="${group}"]`)
  await expect(blocked).toHaveAttribute('data-connect', 'no')
  await expect(blocked).toContainText('⚠ cannot connect')
  await expect(map.locator(`[data-role="${database}_demo"]`)).toHaveCount(0)
  await expect(map.locator(`[data-line="#database>${database}_schema_owner"]`)).toHaveAttribute(
    'stroke-dasharray',
    'none',
  )
  await expect(map.locator(`[data-line="#database>${group}"]`)).toHaveAttribute(
    'data-dashed',
    'true',
  )
  await expect(map.locator(`[data-role="${login}"]`)).toHaveCount(1)
  await expect(map.locator(`[data-line="${group}>${login}"]`)).toHaveCount(1)
  await expect(map.locator(`[data-line="${second}>${login}"]`)).toHaveCount(1)

  // Each line runs from its own source card to its own target card, and the
  // two groups' lines to the shared login turn in lanes of their own.
  const ends = async (key: string) => {
    const d = (await map.locator(`[data-line="${key}"]`).getAttribute('d')) ?? ''
    const [, x1, y1, turn, y2, x2] = /M(-?\d+) (-?\d+) H(-?\d+) V(-?\d+) H(-?\d+)/u
      .exec(d)
      ?.map(Number) ?? [0, 0, 0, 0, 0, 0]
    return { x1, y1, turn, y2, x2 }
  }
  const centre = async (role: string) => {
    const card = await map.locator(`[data-card="${role}"]`).boundingBox()
    const origin = await map.getByTestId('connectors').boundingBox()
    return card === null || origin === null ? NaN : card.y + card.height / 2 - origin.y
  }
  for (const [from, to] of [
    [group, login],
    [second, login],
  ] as const) {
    const line = await ends(`${from}>${to}`)
    expect(Math.abs((line.y1 ?? 0) - (await centre(from))), `${from} end`).toBeLessThanOrEqual(1)
    expect(Math.abs((line.y2 ?? 0) - (await centre(to))), `${to} end`).toBeLessThanOrEqual(1)
  }
  expect((await ends(`${group}>${login}`)).turn).not.toBe((await ends(`${second}>${login}`)).turn)

  // Pointing at a card lights its own lines and fades the others.
  await map.locator(`[data-card="${second}"]`).hover()
  await expect(map.locator(`[data-line="${second}>${login}"]`)).toHaveAttribute(
    'data-emphasis',
    'lit',
  )
  await expect(map.locator(`[data-line="${group}>${login}"]`)).toHaveAttribute(
    'data-emphasis',
    'dim',
  )
})

test('FEAT-076 AC-18: choosing a role opens it in a dialog, by keyboard too', async ({
  page,
  baseURL,
}) => {
  const database = unique('e2edb')
  const group = unique('role_e2e')
  await releasedWithGrant(baseURL ?? '', database, group)

  await searchRoles(page, database)

  const card = page.getByTestId('roles-map').locator(`[data-role="${group}"]`)
  await card.focus()
  await page.keyboard.press('Space')
  const dialog = page.getByRole('dialog', { name: group })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByTestId('dialog-status')).toHaveText('⚠ cannot connect')
  const round = dialog.locator('[data-table="public.round"]')
  await expect(round.locator('[data-privilege="SELECT"]')).toHaveText('✓')
  await expect(round.locator('[data-privilege="DELETE"]')).toHaveText('—')

  await dialog.getByRole('button', { name: 'Grant connect' }).click()
  await expect(dialog.getByTestId('dialog-status')).toHaveText('✓ connect')
  await expect(card).toHaveAttribute('data-connect', 'yes')

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(card).toBeFocused()
})

test('FEAT-076 AC-18: the role dialog closes from its backdrop and its ✕, and reopens without asking again', async ({
  page,
  baseURL,
}) => {
  const database = unique('e2edb')
  const group = unique('role_e2e')
  await releasedWithGrant(baseURL ?? '', database, group)
  const reads: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/access?')) reads.push(request.url())
  })

  await searchRoles(page, database)
  const card = page.getByTestId('roles-map').locator(`[data-role="${group}"]`)
  const dialog = page.getByRole('dialog', { name: group })

  await card.click()
  await expect(dialog.locator('[data-table="public.round"]')).toBeVisible()
  await page.mouse.click(5, 5)
  await expect(dialog).toBeHidden()

  await card.click()
  await expect(dialog.locator('[data-table="public.round"]')).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toBeHidden()
  await expect(card).toBeFocused()

  expect(reads, 'the second opening came from what the first read').toHaveLength(1)
})

test('FEAT-076 AC-18: a login matrix marks what it inherits, and the owner reads as owning everything', async ({
  page,
  baseURL,
}) => {
  const database = unique('e2edb')
  const group = unique('role_e2e')
  await releasedWithGrant(baseURL ?? '', database, group)
  const login = unique('e2e_app')
  const dba = await apiAs(baseURL ?? '', 'dba')
  const created = await dba.post('/api/v1/provision/logins', {
    data: { name: login, memberOf: [group] },
  })
  expect(created.ok()).toBe(true)

  await searchRoles(page, database)
  const map = page.getByTestId('roles-map')

  await map.locator(`[data-role="${login}"]`).click()
  const loginDialog = page.getByRole('dialog', { name: login })
  await expect(loginDialog.getByTestId('dialog-member-of')).toContainText(group)
  await expect(
    loginDialog.locator('[data-table="public.round"] [data-privilege="SELECT"]'),
  ).toHaveText('✓¹')
  await expect(loginDialog.getByTestId('dialog-footnotes')).toContainText(`¹ via ${group}`)
  await expect(loginDialog.getByRole('button', { name: 'New password' })).toBeVisible()
  await expect(loginDialog.getByRole('button', { name: 'Grant connect' })).toHaveCount(0)
  await page.keyboard.press('Escape')

  await map.locator(`[data-role="${database}_schema_owner"]`).click()
  const ownerDialog = page.getByRole('dialog', { name: `${database}_schema_owner` })
  await expect(ownerDialog.getByTestId('dialog-owns')).toHaveText(
    'Owns all 1 table — every privilege',
  )
  await expect(ownerDialog.getByTestId('dialog-tables')).toHaveCount(0)
  await expect(ownerDialog.getByRole('button', { name: 'Drop' })).toHaveCount(0)
})

/*
 * Forty logins in five groups, answered by the page's own API route so the
 * size is exact. Each group shows six logins and one "+2 logins" card, the
 * logins column scrolls on its own, no two cards overlap, and a line still
 * lands on its card once the column has scrolled.
 */
test('FEAT-076 AC-17: forty logins in five groups stay readable and their lines stay attached', async ({
  page,
}) => {
  const grant = (extra: Record<string, unknown>) => ({
    database: 'crowded',
    tables: 4,
    select: 4,
    insert: 0,
    update: 0,
    delete: 0,
    canConnect: true,
    owned: 0,
    ...extra,
  })
  const groups = Array.from({ length: 5 }, (_, g) => `role_team_${g}`)
  const roles = [
    {
      name: 'crowded_schema_owner',
      kind: 'owner',
      canLogin: false,
      memberOf: [],
      members: [],
      grants: [grant({ owned: 4 })],
    },
    ...groups.map((name, g) => ({
      name,
      kind: 'group',
      canLogin: false,
      memberOf: [],
      members: Array.from({ length: 8 }, (_, i) => `svc_${g}_${i}`),
      grants: [grant({ canConnect: g !== 4 })],
    })),
    ...groups.flatMap((group, g) =>
      Array.from({ length: 8 }, (_, i) => ({
        name: `svc_${g}_${i}`,
        kind: 'login',
        canLogin: true,
        memberOf: [group],
        members: [],
        grants: [grant({})],
      })),
    ),
  ]
  await page.route('**/api/v1/provision/roles?*', (route) => route.fulfill({ json: roles }))

  await searchRoles(page, 'crowded')

  const map = page.getByTestId('roles-map')
  await expect(map.locator('[data-more]')).toHaveCount(5)
  await expect(map.locator('[data-more="role_team_4"]')).toContainText('+2 logins')
  await expect(map.locator('[data-column="logins"] [data-card]')).toHaveCount(35)

  const overlaps = await map.locator('[data-card]').evaluateAll((cards) => {
    const boxes = cards.map((card) => card.getBoundingClientRect())
    return boxes.flatMap((a, i) =>
      boxes
        .slice(i + 1)
        .filter(
          (b) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom,
        ),
    ).length
  })
  expect(overlaps, 'cards drawn on top of each other').toBe(0)

  const column = map.locator('[data-column="logins"]')
  expect(await column.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true)
  await column.evaluate((el) => el.scrollTo({ top: el.scrollHeight }))
  const last = map.locator('[data-more="role_team_4"]')
  await expect
    .poll(async () => {
      const card = await last.boundingBox()
      const origin = await map.locator('[data-testid="connectors"]').boundingBox()
      const d = await map.locator('[data-line="role_team_4>+role_team_4"]').getAttribute('d')
      const y = Number(/V(-?\d+)/u.exec(d ?? '')?.[1])
      return card === null || origin === null
        ? Infinity
        : Math.abs(card.y + card.height / 2 - origin.y - y)
    })
    .toBeLessThanOrEqual(1)
})
