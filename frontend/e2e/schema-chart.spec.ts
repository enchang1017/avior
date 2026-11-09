import { expect, test, type Page } from '@playwright/test'

import { serveFixedApi } from './visual/api'
import { answer, answerJson, calls, largeGraph, record, smallGraph } from './redesign/fixtures'

const chart = (page: Page) => page.getByTestId('schema-chart')
const centre = (page: Page) => chart(page).locator('circle[data-ring="0"]')

async function pickDatabase(page: Page): Promise<void> {
  await page.getByRole('button', { name: /aviorfixture/u }).click()
}

async function pickTable(page: Page, name: string): Promise<void> {
  await page
    .locator('button.node')
    .filter({ hasText: new RegExp(`^\\s*${name}\\s*public`, 'u') })
    .click()
}

test.beforeEach(async ({ page }) => {
  await serveFixedApi(page)
})

test('the chart and the tree select the same object', async ({ page }) => {
  await answerJson(page, '/schema/graph', smallGraph)
  await page.goto('/')
  await pickDatabase(page)
  await expect(chart(page).locator('circle.star')).toHaveCount(3)
  await expect(centre(page)).toHaveCount(0)

  await pickTable(page, 'member')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')
  await expect(chart(page)).toHaveAttribute(
    'aria-label',
    /member references nothing and is referenced by bet and member_summary/u,
  )

  await chart(page).locator('circle[data-key="public.bet"]').click()
  await expect(page.locator('button.node.current')).toContainText('bet')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.bet')
  await expect(page.locator('.cm-content')).toContainText('SELECT')
})

test('the tree works while the graph is still loading', async ({ page }) => {
  await answer(page, '/schema/graph', () => new Promise(() => undefined))
  await page.goto('/')
  await pickDatabase(page)
  await expect(page.getByTestId('chart-panel').locator('.chart-placeholder')).toBeVisible()
  await pickTable(page, 'member')
  await expect(page.locator('button.node.current')).toContainText('member')
  await expect(page.locator('.cm-content')).toContainText(
    'SELECT id, username FROM public.member LIMIT 100',
  )
  await expect(chart(page)).toHaveCount(0)
})

test('a collapsed chart stays collapsed and asks for nothing', async ({ page }) => {
  await answerJson(page, '/schema/graph', smallGraph)
  await page.goto('/')
  await page.getByTestId('chart-panel').getByRole('button', { name: 'Hide' }).click()

  const seen = record(page)
  await page.reload()
  await pickDatabase(page)
  await pickTable(page, 'member')
  await expect(page.locator('button.node.current')).toContainText('member')
  await expect(page.getByTestId('chart-panel').getByRole('button', { name: 'Show' })).toBeVisible()
  await expect(chart(page)).toHaveCount(0)
  expect(calls(seen, 'GET', '/schema/graph')).toHaveLength(0)
})

test('a large database draws the target and its neighbours and says so', async ({ page }) => {
  await answerJson(page, '/schema/graph', largeGraph())
  await page.goto('/')
  await pickDatabase(page)
  await pickTable(page, 'member')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')
  const stars = chart(page).locator('circle.star')
  await expect(stars).toHaveCount(150)
  expect(await chart(page).locator('line').count()).toBeLessThanOrEqual(300)
  expect(await chart(page).locator('*').count()).toBeLessThanOrEqual(700)
  await expect(page.getByTestId('chart-panel')).toContainText('showing 150 of 612')
  await expect(
    chart(page).locator('circle[data-key^="public.loose"], circle[data-key^="archive."]'),
  ).toHaveCount(0)
})

test('the caption fits the pane and holds the whole of itself on hover', async ({ page }) => {
  await answerJson(page, '/schema/graph', largeGraph())
  await page.goto('/')
  await pickDatabase(page)
  await pickTable(page, 'member')
  const caption = page.getByTestId('chart-panel').locator('figcaption')
  await expect(caption).toHaveAttribute(
    'title',
    'aviorfixture · 150 objects · fk/view edges · showing 150 of 612',
  )
  const style = await caption.evaluate((element) => {
    const computed = getComputedStyle(element)
    return `${computed.whiteSpace} ${computed.overflowX} ${computed.textOverflow}`
  })
  expect(style).toBe('nowrap hidden ellipsis')
})

type Legibility = { label: number; edge: number; contrast: number; near: number }

function legibility(page: Page): Promise<Legibility> {
  return chart(page).evaluate((svg: SVGSVGElement) => {
    const scale = svg.getScreenCTM()?.a ?? 1
    const channels = (colour: string) => (colour.match(/[\d.]+/gu) ?? []).slice(0, 3).map(Number)
    const luminance = (colour: string) => {
      const [r, g, b] = channels(colour).map((value) => {
        const unit = value / 255
        return unit <= 0.03928 ? unit / 12.92 : ((unit + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (b ?? 0)
    }
    const ratio = (a: string, b: string) => {
      const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x)
      return ((light ?? 0) + 0.05) / ((dark ?? 0) + 0.05)
    }
    let ground = 'rgb(0, 0, 0)'
    for (let at: Element | null = svg; at !== null; at = at.parentElement) {
      const background = getComputedStyle(at).backgroundColor
      if (background !== 'rgba(0, 0, 0, 0)' && background !== 'transparent') {
        ground = background
        break
      }
    }
    const width = (line: Element) => {
      const style = getComputedStyle(line)
      const drawn = Number.parseFloat(style.strokeWidth)
      return style.vectorEffect === 'non-scaling-stroke' ? drawn : drawn * scale
    }
    const lines = [...svg.querySelectorAll('line[data-edge]')]
    const far = lines.find((line) => !line.classList.contains('stroke-chart-edge-near')) ?? lines[0]
    const close =
      lines.find((line) => line.classList.contains('stroke-chart-edge-near')) ?? lines[0]
    const label = svg.querySelector('g.node text') as SVGTextElement
    return {
      label: Number.parseFloat(getComputedStyle(label).fontSize) * scale,
      edge: Math.min(...lines.map(width)),
      contrast: ratio(getComputedStyle(far as Element).stroke, ground),
      near: ratio(getComputedStyle(close as Element).stroke, ground),
    }
  })
}

test('labels read at the smallest type size and edges hold 3:1 in both themes', async ({
  page,
}) => {
  await page.goto('/')
  await pickDatabase(page)
  await pickTable(page, 'member')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')
  const smallest = await page.evaluate(
    () =>
      Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--text-3xs')) *
      Number.parseFloat(getComputedStyle(document.documentElement).fontSize),
  )
  for (const theme of ['dark', 'light'] as const) {
    if (theme === 'light') await page.getByRole('button', { name: 'Theme: Dark' }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    const measured = await legibility(page)
    expect(measured.label, `${theme} label size`).toBeGreaterThanOrEqual(smallest - 0.05)
    expect(measured.edge, `${theme} edge width`).toBeGreaterThanOrEqual(1)
    expect(measured.contrast, `${theme} edge contrast`).toBeGreaterThanOrEqual(3)
    expect(measured.near, `${theme} near edge contrast`).toBeGreaterThanOrEqual(3)
  }
})

/** The console keeps its tabs across a reload, so the database may already be open. */
async function reopenDatabase(page: Page): Promise<void> {
  const database = page.locator('button.node.database', { hasText: 'aviorfixture' })
  await expect(database).toBeVisible()
  if (!(await database.evaluate((element) => element.classList.contains('open')))) {
    await database.click()
  }
}

const isolatedBet = {
  ...smallGraph,
  edges: smallGraph.edges.filter((edge) => edge.from.name !== 'bet' && edge.to.name !== 'bet'),
}

test('an empty target collapses the chart without forgetting the choice', async ({ page }) => {
  await answerJson(page, '/schema/graph', isolatedBet)
  await page.goto('/')
  const panel = page.getByTestId('chart-panel')
  const toggle = panel.locator('button.chart-toggle')
  await pickDatabase(page)
  await expect(chart(page).locator('circle.star')).toHaveCount(3)

  await pickTable(page, 'bet')
  await expect(panel.locator('.chart-isolated')).toHaveText('No relationships · Open in Lineage')
  await expect(panel.locator('svg:not(.lucide)')).toHaveCount(0)
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  expect(await page.evaluate(() => localStorage.getItem('avior.schema-chart.collapsed'))).not.toBe(
    'true',
  )

  await pickTable(page, 'member')
  await expect(panel.locator('.chart-isolated')).toHaveCount(0)
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')

  await toggle.click()
  const seen = record(page)
  await page.reload()
  await reopenDatabase(page)
  await pickTable(page, 'bet')
  await expect(page.locator('button.node.current')).toContainText('bet')
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(panel.locator('.chart-isolated')).toHaveCount(0)
  expect(calls(seen, 'GET', '/schema/graph')).toHaveLength(0)

  await toggle.click()
  await expect(panel.locator('.chart-isolated')).toBeVisible()
  await pickTable(page, 'member')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')
  expect(await page.evaluate(() => localStorage.getItem('avior.schema-chart.collapsed'))).toBe(
    'false',
  )
})

test('the caption opens the target in lineage', async ({ page }) => {
  await answerJson(page, '/schema/graph', isolatedBet)
  await page.goto('/')
  await pickDatabase(page)
  await pickTable(page, 'member')
  await expect(centre(page)).toHaveAttribute('data-key', 'public.member')
  await page.getByTestId('chart-panel').getByRole('link', { name: 'Open in Lineage' }).click()
  await expect(page).toHaveURL(/\/lineage\?/u)
  const address = new URL(page.url()).searchParams
  expect(address.get('database')).toBe('aviorfixture')
  expect(['schema', 'name', 'kind'].map((key) => address.get(key))).toEqual([
    'public',
    'member',
    'table',
  ])
  expect(address.get('focus')).toBeNull()
  await expect(
    page.getByTestId('lineage-chart').locator('[data-node][data-tier="focus"]'),
  ).toHaveAttribute('data-id', 'table:"public"."member"')

  await page.goBack()
  await reopenDatabase(page)
  await pickTable(page, 'bet')
  await page.getByTestId('chart-panel').locator('.chart-isolated a').click()
  await expect.poll(() => new URL(page.url()).searchParams.get('name')).toBe('bet')
})
