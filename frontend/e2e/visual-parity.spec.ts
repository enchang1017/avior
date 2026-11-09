import { existsSync } from 'node:fs'

import { expect, test } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'

import { administrator, serveFixedApi } from './visual/api'
import { addressOf, nodes } from './visual/lineage'

/*
 * What the pages look like, held still.
 *
 * The baselines beside this file are the star chart, captured deliberately in a
 * commit of their own, and every case here asserts the page still draws that
 * way. A token renamed, a utility that resolves to a different
 * value, a rule that stopped applying because the class it hung on was deleted:
 * each is a pixel difference, and none is a failing type or a failing unit test.
 *
 * The API is answered from fixtures and the clock is stopped, so the only thing
 * that can move a pixel is the styling. Both palettes are drawn, each against
 * the opposite system preference, because the console ignores the system and
 * that is exactly what a regression would stop doing.
 *
 * No pixel may differ. A ratio was tried first, and 0.2% of a page this tall is
 * every icon on it: an icon failed to load and the comparison passed. Two
 * runs of one build on one machine draw identical pixels, so any difference is
 * real. Text is rasterised differently per operating system and per installed
 * fonts, which is why there is one baseline per platform — darwin for a
 * developer's machine, linux for the pipeline's Playwright image — rather than a
 * tolerance loose enough to cover both.
 */

const tolerance = { maxDiffPixels: 0, animations: 'disabled' as const }

type Scene = {
  name: string
  path: string
  signedIn?: boolean
  /** The deployment the page believes it is on; the fixture's is staging. */
  environment?: 'production'
  arrange?: (page: Page) => Promise<void>
  /** What the page prints that depends on where it is served from, not how it is styled. */
  varies?: (page: Page) => Locator[]
  /**
   * Captured only once the chart is drawn and the page has stopped changing,
   * so a part that mounts late, on a timer or a second request, is in the
   * picture rather than missed by it.
   */
  settle?: boolean
}

const quietMs = 1500

/** Resolves once the page has gone quietMs without a DOM change, and fails if it never does. */
async function quiet(page: Page): Promise<void> {
  await page.evaluate(
    (span) =>
      new Promise<void>((resolve, reject) => {
        const done = () => {
          observer.disconnect()
          clearTimeout(deadline)
          resolve()
        }
        let timer = setTimeout(done, span)
        const observer = new MutationObserver(() => {
          clearTimeout(timer)
          timer = setTimeout(done, span)
        })
        const deadline = setTimeout(() => {
          observer.disconnect()
          reject(new Error('the page never stopped changing'))
        }, span * 8)
        observer.observe(document.body, {
          subtree: true,
          childList: true,
          attributes: true,
          characterData: true,
        })
      }),
    quietMs,
  )
}

async function pick(page: Page, control: string, label: string): Promise<void> {
  await page.locator(control).click()
  await page.getByRole('option', { name: label, exact: true }).click()
}

const scenes: Scene[] = [
  { name: 'login', path: '/login', signedIn: false },
  {
    name: 'query-console',
    path: '/',
    arrange: async (page) => {
      await page.getByRole('button', { name: /aviorfixture/ }).click()
      await page.getByRole('button', { name: /^member\s+public/u }).click()
      await expect(page.locator('.cm-content')).toContainText('LIMIT 100')
      await page.getByRole('button', { name: 'Execute' }).click()
      await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
    },
  },
  {
    name: 'query-console-production',
    path: '/',
    environment: 'production',
    arrange: async (page) => {
      await page.getByRole('button', { name: /aviorfixture/ }).click()
      await page.getByRole('button', { name: /^member\s+public/u }).click()
      await expect(page.locator('.cm-content')).toContainText('LIMIT 100')
      await page.getByRole('button', { name: 'Execute' }).click()
      await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
    },
  },
  {
    name: 'palette',
    path: '/writes',
    arrange: async (page) => {
      await page.keyboard.press('ControlOrMeta+k')
      await expect(page.getByRole('dialog', { name: 'Find' })).toBeVisible()
    },
  },
  {
    name: 'guard-rules',
    path: '/',
    arrange: async (page) => {
      await page.getByRole('button', { name: /Guarded/ }).click()
      await expect(page.getByRole('dialog', { name: 'What the guard checks' })).toBeVisible()
    },
  },
  {
    name: 'login-refused',
    path: '/login',
    signedIn: false,
    arrange: async (page) => {
      await page.getByLabel('Username').fill('nobody')
      await page.getByRole('button', { name: 'Sign in' }).click()
      await expect(page.getByText('That username and password do not match.')).toBeVisible()
    },
  },
  {
    name: 'guard-verdict',
    path: '/',
    arrange: async (page) => {
      await page.getByRole('button', { name: /aviorfixture/ }).click()
      await page.getByRole('button', { name: /^member\s+public/u }).click()
      await expect(page.locator('.cm-content')).toContainText('LIMIT 100')
      await page.getByRole('button', { name: 'Inspect' }).click()
      await expect(page.getByText('This column is restricted.')).toBeVisible()
    },
  },
  {
    name: 'order-by-open',
    path: '/',
    arrange: async (page) => {
      await page.getByRole('button', { name: /aviorfixture/ }).click()
      await page.getByRole('button', { name: /^member\s+public/u }).click()
      await page.locator('#order-column').click()
      await expect(page.getByRole('listbox')).toBeVisible()
    },
  },
  {
    name: 'audit',
    path: '/audit/all',
    arrange: (page) => expect(page.getByText('SELECT * FROM public.member')).toBeVisible(),
  },
  {
    name: 'audit-calendar',
    path: '/audit/all',
    arrange: async (page) => {
      await page.locator('#audit-from').click()
      await expect(page.locator('.day').first()).toBeVisible()
    },
  },
  { name: 'release-queue', path: '/releases' },
  { name: 'release-detail', path: '/releases/rel-0001-aaaa-bbbb' },
  { name: 'release-blocked', path: '/releases/rel-0004-eeee' },
  {
    name: 'provisioning',
    path: '/releases/provisioning',
    arrange: async (page) => {
      await page.locator('#provision-database').fill('aviorfixture')
      await page.getByRole('button', { name: 'Check' }).click()
      await page.locator('#roles-database').fill('aviorfixture')
      await page.getByRole('button', { name: 'Search' }).click()
      await expect(page.getByText('svc_reporting').first()).toBeVisible()
    },
  },
  {
    name: 'role-dialog',
    path: '/releases/provisioning',
    arrange: async (page) => {
      await page.locator('#roles-database').fill('aviorfixture')
      await page.getByRole('button', { name: 'Search' }).click()
      await page.locator('[data-role="aviorfixture_write"]').click()
      await expect(page.getByTestId('dialog-tables')).toContainText('public.bet')
      await expect(page.getByTestId('dialog-footnotes')).toBeVisible()
    },
  },
  {
    name: 'password-reveal',
    path: '/releases/provisioning',
    arrange: async (page) => {
      await page.locator('#roles-database').fill('aviorfixture')
      await page.getByRole('button', { name: 'Search' }).click()
      await page.getByRole('button', { name: 'New login' }).click()
      await page.locator('#login-name').fill('svc_billing')
      await page.getByRole('button', { name: 'Create login' }).click()
      await expect(page.getByText('correct-horse-battery-staple-42')).toBeVisible()
    },
  },
  {
    name: 'applied-migrations',
    path: '/releases/applied',
    arrange: (page) => pick(page, '#applied-database', 'aviorfixture'),
  },
  { name: 'write-compose', path: '/writes/new' },
  { name: 'write-requests', path: '/writes' },
  {
    name: 'write-requests-undo',
    path: '/writes?open=wr-4444-5555-6666',
    arrange: (page) =>
      expect(page.getByTestId('request-detail').getByText('To undo it')).toBeVisible(),
  },
  {
    name: 'select-open',
    path: '/writes',
    arrange: async (page) => {
      await page.locator('#write-mine').click()
      await expect(page.getByRole('listbox')).toBeVisible()
    },
  },
  {
    name: 'account-menu',
    path: '/writes',
    arrange: async (page) => {
      await page.getByRole('button', { name: 'Your account' }).click()
      await expect(page.getByRole('menu').getByRole('button', { name: 'Sign out' })).toBeVisible()
    },
  },
  {
    name: 'dialog',
    path: '/admin/columns',
    arrange: async (page) => {
      await page.getByRole('button', { name: 'Restrict a column' }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
    },
  },
  {
    name: 'write-request-detail',
    path: '/writes/queue',
    arrange: async (page) => {
      await expect(page.getByTestId('request-detail').getByText('Approval steps')).toBeVisible()
    },
  },
  { name: 'write-captures', path: '/writes/captures' },
  {
    name: 'table-structure',
    path: '/objects/table',
    arrange: async (page) => {
      await pick(page, '#table-database', 'aviorfixture')
      await page.getByRole('button', { name: /member\s+public/ }).click()
      await expect(page.getByRole('heading', { name: /public\.member/ })).toBeVisible()
    },
  },
  {
    name: 'object-viewer',
    path: '/objects/view',
    arrange: async (page) => {
      await pick(page, '#database-view', 'aviorfixture')
      await page.getByRole('button', { name: /member_summary/ }).click()
      await expect(page.locator('.cm-content, pre').first()).toContainText('CREATE VIEW')
    },
  },
  {
    name: 'lineage',
    settle: true,
    path: `/lineage?${new URLSearchParams({ database: 'aviorfixture', ...addressOf(nodes.bet), hops: '2', column: 'status' }).toString()}`,
    arrange: async (page) => {
      await expect(page.getByTestId('column-panel').locator('.counts')).toBeVisible()
      await expect(
        page.getByTestId('column-panel').getByRole('region', { name: 'Indirect' }),
      ).toBeVisible()
    },
  },
  {
    name: 'lineage-path',
    settle: true,
    path: `/lineage?${new URLSearchParams({ database: 'aviorfixture', ...addressOf(nodes.bet), mode: 'path', toSchema: 'promo', toName: 'campaign', toKind: 'table' }).toString()}`,
    arrange: async (page) => {
      await expect(page.getByTestId('path-panel').locator('button.route').first()).toBeVisible()
    },
  },
  {
    name: 'lineage-routine',
    settle: true,
    path: `/lineage?${new URLSearchParams({ database: 'aviorfixture', ...addressOf(nodes.closeMember) }).toString()}`,
    arrange: async (page) => {
      await expect(
        page.getByTestId('routine-panel').getByRole('region', { name: 'Fires' }),
      ).toBeVisible()
    },
  },
  { name: 'profile', path: '/account' },
  {
    name: 'agent-setup',
    path: '/account/agent',
    // The command names this page's own origin, which is a different port on a
    // developer's machine than in the stack.
    varies: (page) => [page.getByText(/api\/v1\/mcp/u)],
  },
  { name: 'tokens', path: '/account/tokens' },
  {
    name: 'token-minted',
    path: '/account/tokens',
    arrange: async (page) => {
      await page.getByLabel('Name').fill('agent')
      await page.getByRole('button', { name: 'Create token' }).click()
      await expect(page.getByText('avior_pat_0123456789abcdef0123456789abcdef')).toBeVisible()
    },
  },
  { name: 'operators', path: '/admin/accounts' },
  { name: 'service-accounts', path: '/admin/service-accounts' },
  {
    name: 'service-token-mint',
    path: '/admin/service-accounts',
    arrange: async (page) => {
      await page.getByRole('button', { name: 'New token' }).first().click()
      await expect(page.getByRole('dialog')).toBeVisible()
    },
  },
  {
    name: 'service-token-minted',
    path: '/admin/service-accounts',
    arrange: async (page) => {
      await page.getByRole('button', { name: 'New token' }).first().click()
      await page.locator('#token-name').fill('nightly')
      await page.getByRole('button', { name: 'Mint token' }).click()
      await expect(page.getByText('avior_pat_0123456789abcdef0123456789abcdef')).toBeVisible()
    },
  },
  { name: 'database-access', path: '/admin/databases' },
  { name: 'column-restrictions', path: '/admin/columns' },
]

type Look = {
  scheme: 'light' | 'dark'
  /** The stored choice, which becomes data-theme. Absent means the default, night. */
  choice?: 'light' | 'dark'
}

const looks: Record<string, Look> = {
  // Night is the default, whatever the system says; day is a choice. Each is
  // drawn against the opposite system preference, which is the case to prove.
  dark: { scheme: 'light' },
  light: { scheme: 'dark', choice: 'light' },
}

async function prepare(page: Page, look: Look): Promise<void> {
  await page.clock.setFixedTime(new Date('2026-09-20T10:00:00Z'))
  await page.emulateMedia({ colorScheme: look.scheme, reducedMotion: 'reduce' })
  const choice = look.choice
  if (choice !== undefined) {
    await page.addInitScript((theme) => window.localStorage.setItem('avior.theme', theme), choice)
  }
}

// Tall, because the shell scrolls inside its own panes and a full-page capture
// of a window-high layout stops at the fold.
test.use({ timezoneId: 'UTC', locale: 'en-US', viewport: { width: 1440, height: 2000 } })

for (const scene of scenes) {
  const drawnIn = ['light', 'dark']

  for (const lookName of drawnIn) {
    const look = looks[lookName]
    if (look === undefined) continue

    test(`${scene.name} looks as it did (${lookName})`, async ({ page }) => {
      const missed = await serveFixedApi(page, scene.signedIn ?? true)
      const environment = scene.environment
      if (environment !== undefined) {
        await page.route('**/api/v1/auth/me', (route) =>
          route.fulfill({ json: { ...administrator, environment } }),
        )
      }
      await prepare(page, look)
      await page.goto(scene.path)
      await page.waitForLoadState('networkidle')
      if (scene.arrange) await scene.arrange(page)
      await page.waitForLoadState('networkidle')
      // Away from whatever was last clicked, so no control is drawn hovered.
      await page.mouse.move(1439, 1999)
      // Fonts arrive after the load event, and a glyph drawn in the fallback
      // face is a different picture of the same styles.
      await page.evaluate(() => document.fonts.ready.then(() => undefined))
      if (scene.settle === true) {
        await expect(page.locator('.lineage-chart [data-node]').first()).toBeVisible()
        await quiet(page)
      }

      expect(missed, 'every request the page made was answered').toEqual([])
      await expect(page).toHaveScreenshot(`${scene.name}-${lookName}.png`, {
        ...tolerance,
        fullPage: true,
        mask: scene.varies?.(page) ?? [],
      })
    })
  }
}

// FEAT-065 AC-16: no scene may be drawn without a baseline to compare it with,
// in either palette, on either platform the suite runs on.
test('every scene has a baseline in both schemes on both platforms', () => {
  const folder = new URL('./visual-parity.spec.ts-snapshots/', import.meta.url)
  const missing: string[] = []
  for (const scene of scenes) {
    for (const look of ['light', 'dark']) {
      for (const platform of ['darwin', 'linux']) {
        const file = `${scene.name}-${look}-chromium-${platform}.png`
        if (!existsSync(new URL(file, folder))) missing.push(file)
      }
    }
  }
  expect(missing).toEqual([])
})
