import { expect, test, type Page } from '@playwright/test'

import { administrator, get, serveFixedApi } from './visual/api'

/*
 * FEAT-065: the console's shell and furniture, one case per acceptance
 * criterion this file is named for. The API is answered from the parity
 * fixtures, so what is asserted is the page, not the stack (ADR 0002: the
 * frontend is Playwright's).
 */

type Environment = (typeof administrator)['environment']
type Caller = Partial<typeof administrator>

async function open(page: Page, path = '/', caller: Caller = {}): Promise<void> {
  await serveFixedApi(page, true)
  await page.route('**/api/v1/auth/me', (route) =>
    route.fulfill({ json: { ...administrator, ...caller } }),
  )
  await page.goto(path)
}

async function pickMember(page: Page): Promise<void> {
  await page
    .getByRole('button', { name: /aviorfixture/ })
    .first()
    .click()
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(page.locator('.cm-content')).toContainText('public.member')
}

test.use({ viewport: { width: 1440, height: 900 }, timezoneId: 'UTC', locale: 'en-US' })

test('the rail shows only the areas the caller reaches, each labelled', async ({ page }) => {
  await open(page, '/', {
    roles: ['developer'],
    permissions: ['query.inspect', 'query.execute', 'audit.read.own'],
  })
  const rail = page.locator('.rail')
  const areas = rail.locator('nav > ul > li:not(.edge)')
  await expect(areas).toHaveCount(8)
  await expect(rail.getByRole('link', { name: 'Lineage', exact: true })).toBeVisible()
  await expect(rail.getByRole('link', { name: 'Observatory', exact: true })).toBeVisible()
  await expect(rail.getByRole('link', { name: 'Query', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  )
  await expect(rail.getByRole('button', { name: /Admin/ })).toHaveCount(0)

  const personal = rail.getByRole('link', { name: 'Personal Log', exact: true })
  await personal.hover()
  await expect(personal.locator('.tip')).toBeVisible()
  await page.mouse.move(900, 600)
  await expect(personal.locator('.tip')).toBeHidden()
  await personal.focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Shift+Tab')
  await expect(personal.locator('.tip')).toBeVisible()
})

/*
 * What the operator sees, not what the element reports: its box leaves out a
 * shadow, and a shadow scaled with the bar once drew it 24px taller than the
 * row it marks. So the column the edge is painted in is read back from a
 * screenshot, and the rows in the target colour are the edge.
 */
async function edgeMisalignment(page: Page): Promise<number> {
  const row = await page.evaluate(() => {
    const current =
      document.querySelector('.rail .rail-item[aria-current="page"]') ??
      document.querySelector('.rail .rail-item.current')
    const box = current?.getBoundingClientRect()
    return box === undefined ? null : { y: box.y, height: box.height }
  })
  const rail = await page.locator('.rail').boundingBox()
  if (row === null || rail === null) return Number.POSITIVE_INFINITY
  const shot = await page.screenshot({
    clip: { x: rail.x, y: rail.y, width: 2, height: rail.height },
    animations: 'disabled',
  })
  const painted = await page.evaluate(
    async ([png, target]) => {
      const image = new Image()
      image.src = `data:image/png;base64,${png}`
      await image.decode()
      const canvas = document.createElement('canvas')
      canvas.width = image.width
      canvas.height = image.height
      const context = canvas.getContext('2d')
      if (context === null) return null
      context.drawImage(image, 0, 0)
      const probe = document.createElement('canvas').getContext('2d')
      if (probe === null) return null
      probe.fillStyle = target
      probe.fillRect(0, 0, 1, 1)
      const [r, g, b] = probe.getImageData(0, 0, 1, 1).data
      const pixels = context.getImageData(0, 0, 1, image.height).data
      const lit: number[] = []
      for (let y = 0; y < image.height; y += 1) {
        const at = y * 4
        const distance =
          Math.abs((pixels[at] ?? 0) - (r ?? 0)) +
          Math.abs((pixels[at + 1] ?? 0) - (g ?? 0)) +
          Math.abs((pixels[at + 2] ?? 0) - (b ?? 0))
        if (distance < 48) lit.push(y)
      }
      return lit.length === 0 ? null : { top: lit[0] ?? 0, bottom: (lit.at(-1) ?? 0) + 1 }
    },
    [
      shot.toString('base64'),
      await page.locator('.rail .edge').evaluate((edge) => getComputedStyle(edge).backgroundColor),
    ] as const,
  )
  if (painted === null) return Number.POSITIVE_INFINITY
  return Math.max(
    Math.abs(rail.y + painted.top - row.y),
    Math.abs(rail.y + painted.bottom - (row.y + row.height)),
  )
}

test("the rail's amber edge spans the current row exactly, folded or not, in both themes", async ({
  page,
}) => {
  await open(page, '/objects/table')
  const nav = page.getByRole('navigation', { name: 'Sections' })
  for (const theme of ['dark', 'light']) {
    for (const rail of ['collapsed', 'open']) {
      await page.evaluate(
        ([chosenTheme, chosenRail]) => {
          window.localStorage.setItem('avior.theme', chosenTheme ?? '')
          window.localStorage.setItem('avior.rail', chosenRail ?? '')
        },
        [theme, rail],
      )
      await page.goto('/objects/table')
      await expect
        .poll(() => edgeMisalignment(page), { message: `table, ${rail}, ${theme}` })
        .toBeLessThanOrEqual(1)

      for (const name of ['Query', 'Update Requests', 'Update Query']) {
        await nav.getByRole('link', { name, exact: true }).click()
        await expect(nav.getByRole('link', { name, exact: true })).toHaveAttribute(
          'aria-current',
          'page',
        )
        await expect
          .poll(() => edgeMisalignment(page), { message: `${name}, ${rail}, ${theme}` })
          .toBeLessThanOrEqual(1)
      }
    }
  }
})

function hudPieces(page: Page) {
  const header = page.locator('header.hud-bar')
  return {
    header,
    target: header.locator('.target'),
    find: header.getByRole('button', { name: 'Find a table, request, operator' }),
    page: header.locator('.page'),
    beacon: header.locator('.env'),
    clock: header.locator('.clock'),
    theme: header.getByRole('button', { name: /^Theme:/ }),
    account: header.getByRole('button', { name: 'Your account' }),
  }
}

test('the account menu sits at the right end of the HUD, and the HUD fits at 1280 and 1440 px', async ({
  page,
}) => {
  await open(page, '/', { environment: 'production' as Environment })
  await pickMember(page)
  const hud = hudPieces(page)

  await expect(page.locator('.rail').getByRole('button', { name: 'Your account' })).toHaveCount(0)
  for (const rail of ['folded', 'unfolded']) {
    await expect(hud.account, `visible with the rail ${rail}`).toBeVisible()
    await page.locator('.rail .fold').click()
  }

  await expect(hud.beacon).toContainText('PRODUCTION')
  await expect(hud.beacon).toContainText('READ-ONLY')
  for (const width of [1440, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const order = [hud.page, hud.target, hud.find, hud.beacon, hud.clock, hud.theme, hud.account]
    const boxes = []
    for (const piece of order) {
      const box = await piece.boundingBox()
      expect(box).not.toBeNull()
      if (box !== null) boxes.push(box)
    }
    const header = await hud.header.boundingBox()
    for (const [at, box] of boxes.entries()) {
      const next = boxes[at + 1]
      if (next !== undefined) expect(box.x + box.width).toBeLessThanOrEqual(next.x)
    }
    const last = boxes.at(-1)
    if (header !== null && last !== undefined) {
      expect(last.x + last.width).toBeLessThanOrEqual(header.x + header.width)
    }
    for (const piece of [
      hud.page,
      hud.find.locator('.kbd'),
      hud.beacon.locator('.hud').first(),
      hud.beacon.locator('.hud').last(),
      hud.clock,
    ]) {
      const fit = await piece.evaluate((element) => [element.scrollWidth, element.clientWidth])
      expect(fit[0], `${await piece.textContent()} is not cut short`).toBeLessThanOrEqual(
        fit[1] ?? 0,
      )
    }
    await expect(hud.target).toHaveAttribute(
      'title',
      'Local PostgreSQL / aviorfixture / public.member',
    )

    await hud.account.click()
    const menu = page.getByRole('menu')
    await expect(menu.getByRole('button', { name: 'Sign out' })).toBeVisible()
    const trigger = await hud.account.boundingBox()
    expect(trigger).not.toBeNull()
    if (trigger === null) return
    await expect
      .poll(async () => {
        const panel = await menu.boundingBox()
        return panel === null
          ? null
          : {
              rightAligned: Math.abs(panel.x + panel.width - (trigger.x + trigger.width)) <= 1,
              below: panel.y >= trigger.y + trigger.height,
              onScreen: panel.x >= 0,
            }
      })
      .toEqual({ rightAligned: true, below: true, onScreen: true })
    await page.keyboard.press('Escape')
    await expect(menu).toBeHidden()
  }
})

test('the account menu opens and closes from the keyboard', async ({ page }) => {
  await open(page)
  const hud = hudPieces(page)
  const menu = page.getByRole('menu')

  await hud.theme.focus()
  await page.keyboard.press('Tab')
  await expect(hud.account).toBeFocused()

  await page.keyboard.press('Enter')
  await expect(menu.getByRole('link', { name: 'Account' })).toBeVisible()
  await expect(menu.getByRole('link', { name: 'API tokens' })).toBeVisible()
  await expect(menu.getByRole('link', { name: 'Connect your agent' })).toBeVisible()
  await expect(menu.locator('.chip', { hasText: 'administrator' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(menu).toBeHidden()
  await expect(hud.account).toBeFocused()

  await page.keyboard.press('Space')
  await expect(menu).toBeVisible()
  await page.keyboard.press('Tab')
  await expect(menu.getByRole('link', { name: 'Account' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(menu).toBeHidden()
  await expect(hud.account).toBeFocused()
})

test('the southward mark is the logo and the favicon', async ({ page, request }) => {
  await open(page)
  await expect(page.locator('.rail [role="img"][aria-label="Avior"]')).toBeVisible()
  const icons = await page
    .locator('link[rel="icon"], link[rel="apple-touch-icon"]')
    .evaluateAll((links) => links.map((link) => link.getAttribute('href')))
  expect(icons).toEqual(['/favicon.svg', '/favicon.ico', '/apple-touch-icon.png'])

  const ico = await (await request.get('/favicon.ico')).body()
  const sizes = Array.from({ length: ico.readUInt16LE(4) }, (_, at) => ico.readUInt8(6 + at * 16))
  expect(sizes).toEqual([16, 32, 48])
  const touch = await (await request.get('/apple-touch-icon.png')).body()
  expect([touch.readUInt32BE(16), touch.readUInt32BE(20)]).toEqual([180, 180])
  expect((await request.get('/favicon.svg')).ok()).toBe(true)
})

test('the target readout follows what the console is pointed at', async ({ page }) => {
  await open(page)
  const readout = page.locator('.hud-bar .target .aimed')
  await expect(readout).toHaveText('no target')

  await page
    .getByRole('button', { name: /aviorfixture/ })
    .first()
    .click()
  await expect(readout).toHaveText(`${administrator.instance.name} / aviorfixture`)
  await page.getByRole('button', { name: /^member\s+public/u }).click()
  await expect(readout).toHaveText(`${administrator.instance.name} / aviorfixture / public.member`)
  await expect(page.locator('.tabs .crumb.instance')).toHaveText(administrator.instance.name)
})

test('the beacon is shown on every page in production and nowhere else', async ({ page }) => {
  const signal = (environment: Environment) => page.locator(`.hud-bar .env.${environment}`)
  await open(page, '/', { environment: 'production' })
  for (const path of ['/', '/writes', '/audit/all', '/admin/databases']) {
    await page.goto(path)
    await expect(signal('production').locator('.animate-beacon-sweep')).toBeVisible()
    await expect(page.locator('.beacon-line')).toBeVisible()
  }
  await page.goto('/')
  await expect(signal('production')).toHaveText(/PRODUCTION\s*· READ-ONLY/u)
  await page.goto('/writes')
  await expect(signal('production')).toHaveText('PRODUCTION')

  for (const environment of ['staging', 'development', 'test'] as const) {
    await page.unrouteAll({ behavior: 'ignoreErrors' })
    await open(page, '/', { environment })
    await expect(signal(environment)).toBeVisible()
    await expect(signal(environment).locator('[class*="animate-"]')).toHaveCount(0)
    await expect(page.locator('.beacon-line')).toHaveCount(0)
  }
})

test('statements open in tabs, keep their database, and survive a reload', async ({ page }) => {
  await open(page)
  await pickMember(page)
  const tabs = page.locator('.editor-tab')
  await expect(tabs).toHaveCount(1)
  await expect(tabs.first()).toContainText('aviorfixture')

  await page.getByRole('button', { name: /Open another statement/ }).click()
  await expect(tabs).toHaveCount(2)
  await page.locator('.risk button.trigger').click()
  await page.getByRole('menuitemradio', { name: 'ledger' }).click()
  await page.locator('.cm-content').fill('SELECT 2')
  await expect(tabs.nth(1)).toContainText('ledger')

  await tabs.nth(0).getByRole('tab').click()
  await expect(page.locator('.cm-content')).toContainText('public.member')
  await expect(page.locator('.hud-bar .target .aimed')).toContainText('aviorfixture')

  await page.reload()
  await expect(tabs).toHaveCount(2)
  await tabs.nth(1).getByRole('tab').click()
  await expect(page.locator('.cm-content')).toContainText('SELECT 2')
  await expect(page.locator('.editor-tab.active')).toContainText('ledger')
})

test('a ninth tab is refused with the reason', async ({ page }) => {
  await open(page)
  const another = page.getByRole('button', { name: /Open another statement/ })
  for (let count = 1; count < 8; count += 1) await another.click()
  await expect(page.locator('.editor-tab')).toHaveCount(8)
  await another.click()
  await expect(page.locator('.editor-tab')).toHaveCount(8)
  await expect(page.getByRole('status').filter({ hasText: 'Eight statements' })).toBeVisible()
})

test('the connection bar offers exactly the reachable databases', async ({ page }) => {
  await open(page)
  await page.locator('.risk button.trigger').click()
  const offered = page.getByRole('menuitemradio')
  await expect(offered).toHaveText(['aviorfixture', 'aviordeploy', 'ledger'])
  await offered.filter({ hasText: 'ledger' }).click()
  await expect(page.locator('.hud-bar .target .aimed')).toContainText('ledger')
  await expect(page.locator('.editor-tab.active')).toContainText('ledger')
})

test('an allowed run reads out rows, elapsed, audit id and masked columns', async ({ page }) => {
  await open(page)
  await pickMember(page)
  await page.getByRole('button', { name: 'Execute' }).click()
  const readout = page.locator('.readout')
  await expect(readout).toContainText('ALLOWED')
  await expect(readout).toContainText('ROWS 3')
  await expect(readout).toContainText('ELAPSED 8 MS')
  await expect(readout).toContainText('LOG aud-9')
  await expect(readout).toContainText('MASKED 1 COL')
})

test('a refusal reads out every rule and lists each reason', async ({ page }) => {
  await open(page)
  await pickMember(page)
  await page.getByRole('button', { name: 'Inspect' }).click()
  const readout = page.locator('.readout')
  await expect(readout).toContainText('REFUSED')
  await expect(readout).toContainText('column_denied')
  await expect(readout).toContainText('cost_limit_exceeded')
  await page.getByRole('tab', { name: /Messages/ }).click()
  await expect(page.locator('.messages .rule')).toHaveText(['column_denied', 'cost_limit_exceeded'])
})

test('the scan line plays once for an allowed run and never for a refusal', async ({ page }) => {
  await page.addInitScript(() => {
    const played: number[] = []
    Object.assign(window, { __scans: played })
    const animate = Object.getOwnPropertyDescriptor(Element.prototype, 'animate')?.value as (
      this: Element,
      ...args: Parameters<Element['animate']>
    ) => Animation
    Element.prototype.animate = function (this: Element, ...args) {
      if (this.classList.contains('scan')) {
        played.push(document.querySelectorAll('.result tbody tr[data-row]').length)
      }
      return animate.apply(this, args)
    }
  })
  await open(page)
  await pickMember(page)
  const scans = () => page.evaluate(() => (window as unknown as { __scans: number[] }).__scans)

  await page.getByRole('button', { name: 'Execute' }).click()
  await expect.poll(scans).toHaveLength(1)
  expect((await scans())[0], 'every row is in the grid before it starts').toBe(3)

  await page.getByRole('button', { name: 'Inspect' }).click()
  await expect(page.locator('.readout')).toContainText('REFUSED')
  await page.waitForTimeout(300)
  expect(await scans()).toHaveLength(1)
})

test('update requests list with status tabs, counts and scope chips', async ({ page }) => {
  await open(page, '/writes')
  const tabs = page.getByRole('tablist', { name: 'Request state' }).getByRole('tab')
  await expect(tabs.first()).toHaveText(/All\s*3/u)
  await expect(tabs.filter({ hasText: 'Waiting for review' })).toHaveText(/1/u)
  await expect(tabs.filter({ hasText: 'Applied' })).toHaveText(/1/u)

  const rows = page.locator('[data-request]')
  await expect(rows).toHaveCount(3)
  await page.locator('#write-search').fill('state:applied')
  await expect(page.locator('.scope-search .scope')).toHaveText('state:applied')
  await expect(rows).toHaveCount(1)
  await page.locator('#write-search').fill('database:aviorfixture requester:chen.yu')
  await expect(page.locator('.scope-search .scope')).toHaveCount(2)
  await expect(rows).toHaveCount(3)
  await page.getByRole('button', { name: 'Remove database:aviorfixture' }).click()
  await expect(page.locator('.scope-search .scope')).toHaveText('requester:chen.yu')

  await tabs.filter({ hasText: 'Rejected' }).click()
  await expect(rows).toHaveCount(1)
})

/*
 * A list is read down, so nothing on a row may sit past the edge: the row's
 * action was cut off at the right, and opening a request from it scrolled the
 * list sideways. Every table on these pages fits its container, and every
 * button in it is inside that container. The update requests are a list
 * beside the request being read, and neither half may spill sideways either.
 */
async function expectTablesFit(page: Page): Promise<void> {
  const tables = page.locator('main table, .workspace table, table')
  await expect(tables.first()).toBeVisible()
  const problems = await page.evaluate(() =>
    [...document.querySelectorAll('table')].flatMap((table, at) => {
      const box = table.parentElement as HTMLElement
      const edge = box.getBoundingClientRect()
      const found: string[] = []
      if (box.scrollWidth > box.clientWidth) {
        found.push(`table ${at}: ${box.scrollWidth} px of table in ${box.clientWidth} px`)
      }
      if (box.scrollLeft !== 0) found.push(`table ${at}: scrolled ${box.scrollLeft} px sideways`)
      for (const button of table.querySelectorAll('button')) {
        const { left, right } = button.getBoundingClientRect()
        if (left < edge.left || right > edge.right) {
          found.push(`table ${at}: "${button.textContent?.trim()}" spans ${left}-${right}`)
        }
      }
      return found
    }),
  )
  expect(problems).toEqual([])
}

async function expectPaneFits(page: Page): Promise<void> {
  const problems = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>('.split > section')].flatMap((pane, at) => {
      const edge = pane.getBoundingClientRect()
      const found: string[] = []
      if (pane.scrollWidth > pane.clientWidth) {
        found.push(`pane ${at}: ${pane.scrollWidth} px in ${pane.clientWidth} px`)
      }
      for (const button of pane.querySelectorAll('button')) {
        const { left, right, width } = button.getBoundingClientRect()
        if (width > 0 && (left < edge.left || right > edge.right)) {
          found.push(`pane ${at}: "${button.textContent?.trim()}" spans ${left}-${right}`)
        }
      }
      return found
    }),
  )
  expect(problems).toEqual([])
}

test('request, release, audit and account lists fit, and keep their actions in view', async ({
  page,
}) => {
  const requests = get('/writes')?.body as object[]
  const long = requests.map((request) => ({
    ...request,
    targetTable: 'public.member_loyalty_tier_history',
    requestedBy: 'acceptance-write-requester-7f3c2a91',
    reverseStatement: "UPDATE public.member SET tier = 'silver' WHERE id = 42",
    reason:
      'Customer support ticket 48213: the loyalty tier was recalculated twice during the ' +
      'September migration, so these members were moved to gold and must be put back.',
  }))
  await open(page, '/writes')
  await page.route(/\/api\/v1\/writes(\?.*)?$/u, (route) => route.fulfill({ json: long }))
  await page.reload()
  await expect(page.locator('[data-request]')).toHaveCount(3)
  await expectPaneFits(page)
  await page.locator('[data-request]').first().click()
  await expect(page.getByTestId('request-detail').getByText('Approval steps')).toBeVisible()
  await expectPaneFits(page)

  await page.goto('/releases')
  await expectTablesFit(page)

  for (const path of ['/audit/all', '/admin/service-accounts']) {
    await page.goto(path)
    await expectTablesFit(page)
  }
})

test('the console is dark by default and remembers light', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' })
  await open(page)
  const root = page.locator('html')
  await expect(root).toHaveAttribute('data-theme', 'dark')
  await page.getByRole('button', { name: 'Theme: Dark' }).click()
  await expect(root).toHaveAttribute('data-theme', 'light')
  await page.reload()
  await expect(root).toHaveAttribute('data-theme', 'light')
  await expect(page.getByRole('button', { name: 'Theme: Light' })).toBeVisible()
})

test('reduced motion leaves only a spinner moving', async ({ browser }) => {
  const page = await browser.newPage({ reducedMotion: 'reduce' })
  await open(page, '/', { environment: 'production' })
  await pickMember(page)
  await page.getByRole('button', { name: 'Execute' }).click()
  await expect(page.locator('.readout')).toContainText('ALLOWED')
  await page.waitForTimeout(700)
  const running = await page.evaluate(() =>
    document
      .getAnimations()
      .filter((animation) => animation.playState === 'running')
      .map((animation) => (animation instanceof CSSAnimation ? animation.animationName : 'script')),
  )
  expect(running.filter((name) => name !== 'spin')).toEqual([])
  await expect(page.locator('.env.production')).toBeVisible()
  await page.close()
})

test('every icon is inline svg and no primeicons remain', async ({ page }) => {
  const fetched: string[] = []
  page.on('request', (request) => fetched.push(request.url()))
  await open(page)
  for (const path of ['/', '/writes', '/releases', '/audit/all', '/admin/accounts', '/account']) {
    await page.goto(path)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('svg.lucide').first()).toBeVisible()
    expect(await page.locator('[class*="pi-"], i.pi').count(), path).toBe(0)
  }
  expect(fetched.filter((url) => /primeicons|\.(eot|ttf)(\?|$)/u.test(url))).toEqual([])
})

test('the sign-in fields are named by their labels', async ({ page }) => {
  await serveFixedApi(page, false)
  await page.goto('/login')
  await expect(page.getByRole('textbox', { name: 'Username' })).toBeVisible()
  await expect(page.getByLabel(/^Password/u)).toHaveAttribute('type', 'password')
})

test('the vim toggle says whether it is on', async ({ page }) => {
  await open(page)
  const toggle = page.getByRole('button', { name: 'Vim', exact: true })
  await expect(toggle).toHaveAttribute('aria-pressed', 'false')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-pressed', 'true')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-pressed', 'false')
})

test.describe('in a browser set to another zone and language', () => {
  test.use({ timezoneId: 'Asia/Taipei', locale: 'zh-TW' })

  test('every page prints a moment in UTC, one way, with the full timestamp on hover', async ({
    page,
  }) => {
    await page.clock.setFixedTime(new Date('2026-09-20T10:00:00Z'))
    const printed = async () =>
      page.locator('td[title], dd[title], [data-moment][title]').evaluateAll((cells) =>
        cells
          .map((cell) => ({
            title: cell.getAttribute('title') ?? '',
            text: cell.textContent?.trim(),
          }))
          .filter((cell) => /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/u.test(cell.title)),
      )
    const expected = (iso: string) =>
      iso.startsWith('2026-09-20') ? iso.slice(11, 16) : `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
    for (const path of [
      '/writes',
      '/releases',
      '/audit/all',
      '/admin/service-accounts',
      '/account/tokens',
    ]) {
      await open(page, path)
      await expect(page.locator('table, [data-request]').first()).toBeVisible()
      const cells = await printed()
      expect(cells.length, `${path} prints a moment`).toBeGreaterThan(0)
      for (const cell of cells)
        expect(cell.text, `${path} ${cell.title}`).toBe(expected(cell.title))
    }
  })
})
