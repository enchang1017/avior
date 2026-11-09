import { gzipSync } from 'node:zlib'

import { expect, test, type Page } from '@playwright/test'

import {
  frameWork,
  infiniteAnimations,
  longestSince,
  nextPaint,
  observe,
  settleFirstLoad,
  smoothFrames,
  taskCount,
  throttle,
  vitals,
} from './perf/measure'
import { animationName, budget } from './perf/budget'
import {
  brushSky,
  driveChart,
  markLineagePaint,
  openMember,
  serve,
  serveCapLineage,
  serveLargeGraph,
  serveWideResult,
} from './perf/scene'
import { serveObservatory } from './visual/observatory'

/*
 * ADR 0009's runtime budget, P8 to P18, on a production build served by
 * `vite preview`, with the API answered from fixtures, a cold cache, a
 * 1440x900 viewport and the CPU throttled four times. Every number below is
 * the ADR's; none has a tolerance. FEAT-065 AC-14.
 */

test.use({ viewport: { width: 1440, height: 900 }, timezoneId: 'UTC', locale: 'en-US' })

/*
 * Located by CSS, not by role, inside a measured window. A role query computes
 * the accessible name of every element on the page, on the page's own main
 * thread, and under the four-times throttle that was a 30 to 48 ms task of
 * Playwright's own, counted by P11 as if the page had spent it.
 */
async function runWide(page: Page): Promise<void> {
  await page.locator('button', { hasText: 'Execute' }).click()
  await expect(page.locator('tbody tr[data-row] td').first()).toHaveText('column_1-0')
  await nextPaint(page)
}

async function markKeystrokeReady(page: Page): Promise<void> {
  await page.addInitScript(() => {
    new MutationObserver((_, observer) => {
      const content = document.querySelector('.cm-content')
      if (content?.textContent?.includes('ready?') === true) {
        window.__perf.marks['typed'] = performance.now()
        observer.disconnect()
      }
    }).observe(document, { childList: true, subtree: true, characterData: true })
  })
}

test('the query console is ready within budget', async ({ page }) => {
  await serve(page)
  await observe(page)
  await markKeystrokeReady(page)
  await throttle(page)
  await page.goto('/')
  await page.locator('.cm-content').click()
  await page.keyboard.type('ready?')
  await page.waitForFunction(() => window.__perf.marks['typed'] !== undefined)
  await page.waitForLoadState('networkidle')
  await page.waitForTimeout(500)

  const { lcp, cls } = await vitals(page)
  const loadTasks = await page.evaluate(() => window.__perf.longTasks.map(Math.round))
  console.warn(`load long tasks: ${loadTasks.join(', ') || 'none'}`)
  const typed = Math.round(await page.evaluate(() => window.__perf.marks['typed'] ?? Infinity))
  console.warn(`P8 lcp ${lcp} ms · P9 keystroke ${typed} ms · P10 cls ${cls}`)
  expect(lcp, 'P8 largest contentful paint').toBeLessThanOrEqual(budget.lcpMs)
  expect(typed, 'P9 the editor accepts a keystroke').toBeLessThanOrEqual(budget.keystrokeReadyMs)
  expect(cls, 'P10 layout shift, the font swap included').toBeLessThanOrEqual(budget.cls)
})

async function paletteKeystrokes(page: Page, text: string): Promise<number[]> {
  await page.evaluate(() => {
    const marks = window.__perf.marks
    window.addEventListener(
      'keydown',
      (event) => {
        if (event.key.length !== 1 || event.metaKey || event.ctrlKey) return
        const started = performance.now()
        requestAnimationFrame(() =>
          requestAnimationFrame(() => {
            marks[`key-${Object.keys(marks).length}`] = performance.now() - started
          }),
        )
      },
      { capture: true },
    )
  })
  await page.keyboard.press('ControlOrMeta+k')
  await expect(page.getByTestId('palette')).toBeVisible()
  for (const character of text) {
    await page.keyboard.type(character)
    await nextPaint(page)
  }
  return page.evaluate(() =>
    Object.entries(window.__perf.marks)
      .filter(([name]) => name.startsWith('key-'))
      .map(([, value]) => Math.round(value)),
  )
}

test('no long task during run, search, chart or brush', async ({ page }) => {
  await serve(page)
  await serveWideResult(page)
  await serveLargeGraph(page)
  await observe(page)
  await page.goto('/')
  await openMember(page)
  await expect(page.getByTestId('schema-chart').locator('circle.star')).toHaveCount(
    budget.chart.nodes,
  )
  await throttle(page)

  const beforeRun = await taskCount(page)
  await runWide(page)
  const run = await longestSince(page, beforeRun)

  const beforeSearch = await taskCount(page)
  const keystrokes = await paletteKeystrokes(page, 'member_s')
  const search = await longestSince(page, beforeSearch)
  await page.keyboard.press('Escape')

  const beforeChart = await taskCount(page)
  await driveChart(page)
  await nextPaint(page)
  const chart = await longestSince(page, beforeChart)
  await expect(page.getByTestId('chart-panel').locator('button.chart-toggle')).toHaveAttribute(
    'aria-expanded',
    'false',
  )

  await page.goto('/audit/all')
  await page.getByTestId('audit-sky').locator('svg').waitFor()
  await page.waitForLoadState('networkidle')
  const beforeBrush = await taskCount(page)
  await brushSky(page)
  await page.waitForLoadState('networkidle')
  await nextPaint(page)
  const brush = await longestSince(page, beforeBrush)
  await expect(
    page.getByTestId('audit-sky').locator('rect.brush'),
    'the brush was drawn',
  ).toBeVisible()

  console.warn(
    `P11 run ${run} ms · search ${search} ms · chart ${chart} ms · brush ${brush} ms` +
      ` · P12 keystrokes ${keystrokes.join(', ')} ms`,
  )
  expect(run, 'P11 longest task while 500 x 12 rows render').toBeLessThanOrEqual(budget.longTaskMs)
  expect(search, 'P11 longest task opening the palette and typing').toBeLessThanOrEqual(
    budget.longTaskMs,
  )
  expect(
    chart,
    'P11 longest task expanding, selecting and collapsing 150 stars',
  ).toBeLessThanOrEqual(budget.longTaskMs)
  expect(brush, 'P11 longest task over a 40-step sky brush').toBeLessThanOrEqual(budget.longTaskMs)
  expect(keystrokes, 'P12 one mark per keystroke').toHaveLength(8)
  for (const delay of keystrokes) {
    expect(delay, 'P12 keystroke to painted matches').toBeLessThanOrEqual(budget.paletteKeystrokeMs)
  }
})

async function productionAfterRun(page: Page): Promise<void> {
  await serve(page, 'production')
  await page.clock.setFixedTime(new Date('2026-09-20T10:00:10Z'))
  await page.goto('/')
  await openMember(page)
  await page.getByRole('button', { name: 'Execute' }).click()
  await expect(page.getByRole('cell', { name: 'alice', exact: true })).toBeVisible()
  await page.mouse.move(0, 899)
}

test('the beacon and the scan line cost no layout or paint', async ({ page }) => {
  await productionAfterRun(page)
  // The scan line lasts 600 ms; slowed tenfold it spans the whole window, so
  // the window measures it running, not after it ended.
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Animation.enable')
  await page.waitForTimeout(700)
  await cdp.send('Animation.setPlaybackRate', { playbackRate: 0.1 })
  await page.keyboard.press('ControlOrMeta+Enter')
  await page.waitForTimeout(400)
  const running = await page.evaluate(() => document.getAnimations().length)
  const work = await frameWork(page, budget.quietWindowMs)
  await cdp.send('Animation.setPlaybackRate', { playbackRate: 1 })
  const smooth = await smoothFrames(page, budget.quietWindowMs, budget.smoothFrameMs)
  console.warn(
    `P13 layouts ${work.layouts} · paints ${work.paints} over ${budget.quietWindowMs} ms` +
      ` with ${running} animations · P14 ${Math.round(smooth * 1000) / 10}% frames ≤ ${budget.smoothFrameMs} ms`,
  )
  expect(running, 'the beacon and the scan line are both running').toBeGreaterThanOrEqual(3)
  expect(work.layouts, 'P13 Layout events').toBe(0)
  expect(work.paints, 'P13 Paint events').toBe(0)
  expect(smooth, 'P14 share of frames within 20 ms').toBeGreaterThanOrEqual(budget.smoothShare)
})

test('infinite animations stay within the P15 cap, and none run under reduced motion', async ({
  page,
  browser,
}) => {
  // The cap is P15's, 24 since the user's decision of
  // 2026-09-26 (ADR 0009). The beacon is two of them on production.
  await productionAfterRun(page)
  await page.waitForTimeout(800)
  const moving = (await infiniteAnimations(page)).map(animationName)
  console.warn(`P15 running: ${moving.join(', ') || 'none'}`)
  const cap = budget.motion.maxInfiniteAnimations
  expect(moving.length, 'P15 infinite animations at once').toBeLessThanOrEqual(cap)
  expect(moving).toEqual(expect.arrayContaining(['beacon-pulse', 'beacon-sweep']))

  // One over the cap is a failure: the gate can tell.
  await page.evaluate(
    (extra) => {
      for (let index = 0; index < extra; index += 1) {
        const ring = document.createElement('div')
        document.body.append(ring)
        ring.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 1000, iterations: Infinity })
      }
    },
    cap + 1 - moving.length,
  )
  expect((await infiniteAnimations(page)).length).toBeGreaterThan(cap)

  const calm = await browser.newPage({ reducedMotion: 'reduce' })
  await productionAfterRun(calm)
  await calm.waitForTimeout(800)
  expect(await infiniteAnimations(calm), 'P15 under reduced motion').toEqual([])
  const stillMoving = await calm.evaluate(
    () => document.getAnimations().filter((animation) => animation.playState === 'running').length,
  )
  expect(stillMoving, 'nothing moves: no scan line, no beacon').toBe(0)
  await calm.close()
})

test('the result grid keeps at most sixty rows in the dom', async ({ page }) => {
  await serve(page)
  await serveWideResult(page)
  await page.goto('/')
  await openMember(page)
  await runWide(page)
  await page.waitForTimeout(800)

  const rows = await page.locator('.result tbody tr[data-row]').count()
  const elements = await page.evaluate(() => document.querySelectorAll('*').length)
  console.warn(`P17 rows in the DOM ${rows} · P18 elements ${elements}`)
  expect(rows, 'P17 rows in the DOM of a 500-row result').toBeLessThanOrEqual(budget.gridRows)
  expect(rows).toBeGreaterThan(0)
  expect(elements, 'P18 elements at rest').toBeLessThanOrEqual(budget.consoleElements)

  await page.locator('.result .overflow-auto').evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  await expect(page.getByRole('cell', { name: 'column_1-499', exact: true })).toBeVisible()
  expect(await page.locator('.result tbody tr[data-row]').count()).toBeLessThanOrEqual(
    budget.gridRows,
  )
})

// P16, on a database of 612 objects: the chart draws the target and its
// neighbours up to its cap, and no more.
test('the schema chart stays within its budget', async ({ page }) => {
  await serve(page)
  await serveLargeGraph(page)
  await page.goto('/')
  await openMember(page)
  const chart = page.locator('[data-testid="schema-chart"]')
  await expect(chart.locator('[data-node]').first()).toBeVisible()

  const drawn = await chart.evaluate((element) => ({
    nodes: element.querySelectorAll('[data-node]').length,
    edges: element.querySelectorAll('[data-edge]').length,
    svg: element.querySelectorAll('*').length,
  }))
  console.warn(`P16 nodes ${drawn.nodes} · edges ${drawn.edges} · svg elements ${drawn.svg}`)
  expect(drawn.nodes, 'P16 nodes').toBeGreaterThan(0)
  expect(drawn.nodes, 'P16 nodes').toBeLessThanOrEqual(budget.chart.nodes)
  expect(drawn.edges, 'P16 edges').toBeLessThanOrEqual(budget.chart.edges)
  expect(drawn.svg, 'P16 SVG elements').toBeLessThanOrEqual(budget.chart.svgElements)
})

const lineageChart = (page: Page) => page.getByTestId('lineage-chart')

async function lineageAtCap(page: Page): Promise<void> {
  await serve(page)
  const address = await serveCapLineage(page)
  await observe(page)
  await markLineagePaint(page)
  await throttle(page)
  await page.goto(address)
  await expect(lineageChart(page).locator('[data-node][data-tier="focus"]')).toHaveCount(1)
  await page.waitForLoadState('networkidle')
  await nextPaint(page)
}

const layoutRuns = (page: Page) =>
  page.evaluate(() =>
    performance
      .getEntriesByName('lineage:layout')
      .map((entry) => Math.round(entry.duration * 10) / 10),
  )

// P20, FEAT-070 AC-41: at the server's cap, the page draws what it was sent and no more.
test('the lineage page draws within its node cap and layout budget', async ({ page }) => {
  await lineageAtCap(page)
  const drawn = await lineageChart(page).evaluate((svg) => ({
    nodes: svg.querySelectorAll('[data-node]').length,
    edges: svg.querySelectorAll('[data-edge]').length,
    svg: svg.querySelectorAll('*').length,
  }))
  const layouts = await layoutRuns(page)
  const paint = await page.evaluate(() => {
    const { answered, painted } = window.__perf.marks
    return answered === undefined || painted === undefined
      ? Infinity
      : Math.round(painted - answered)
  })
  console.warn(
    `P20 nodes ${drawn.nodes} · edges ${drawn.edges} · svg elements ${drawn.svg}` +
      ` · layout ${Math.max(...layouts)} ms over ${layouts.length} runs · painted ${paint} ms after its answer`,
  )
  expect(drawn.nodes, 'P20 nodes').toBe(budget.lineage.nodes)
  expect(drawn.edges, 'P20 edges').toBeGreaterThan(250)
  expect(drawn.edges, 'P20 edges').toBeLessThanOrEqual(budget.lineage.edges)
  expect(drawn.svg, 'P20 SVG elements').toBeLessThanOrEqual(budget.lineage.svgElements)
  expect(layouts.length, 'the layout was measured').toBeGreaterThan(0)
  expect(Math.max(...layouts), 'P20 layout, throttled').toBeLessThanOrEqual(budget.lineage.layoutMs)
  expect(paint, 'P20 first paint after the answer').toBeLessThanOrEqual(budget.lineage.paintMs)
})

// P21, FEAT-070 AC-42: every interaction the page offers, at 150 nodes, throttled.
test('lineage interactions produce no long task', async ({ page }) => {
  await lineageAtCap(page)
  const stars = lineageChart(page).locator('[data-node]')
  const measure = async (work: () => Promise<void>) => {
    const before = await taskCount(page)
    await work()
    await page.waitForLoadState('networkidle')
    await nextPaint(page)
    await page.waitForTimeout(450)
    return longestSince(page, before)
  }

  const hover = await measure(async () => {
    for (const index of [3, 17, 42, 88, 120]) await stars.nth(index).hover({ force: true })
    await page.mouse.move(5, 895)
  })
  const focus = await measure(async () => {
    await lineageChart(page).locator('[data-node][data-id*="near_7"]').click({ force: true })
    await expect(lineageChart(page).locator('[data-node][data-tier="focus"]')).toHaveAttribute(
      'data-id',
      /near_7/u,
    )
  })
  const hops = await measure(async () => {
    await page.getByRole('group', { name: 'Hops' }).getByRole('button', { name: '1' }).click()
    await expect(page).not.toHaveURL(/hops=2/u)
    await page.getByRole('group', { name: 'Hops' }).getByRole('button', { name: '2' }).click()
    await expect(page).toHaveURL(/hops=2/u)
  })
  const path = await measure(async () => {
    await page.getByRole('button', { name: 'Path to…' }).click()
    await page.locator('#lineage-to').fill('far_40')
    await page
      .getByRole('option', { name: /far_40\b/u })
      .first()
      .click()
    await page.getByTestId('path-panel').locator('button.route').nth(1).click()
  })
  const overview = await measure(async () => {
    await page.getByRole('button', { name: 'Overview — all schemas' }).click()
    await page.getByTestId('lineage-galaxy').locator('[data-cluster]').first().click()
    await expect(page).not.toHaveURL(/mode=overview/u, { timeout: 3000 })
    await expect(lineageChart(page).locator('[data-node][data-tier="focus"]')).toHaveCount(1)
  })
  console.warn(
    `P21 hover ${hover} ms · focus ${focus} ms · hops ${hops} ms · path ${path} ms · overview and flight ${overview} ms`,
  )
  expect(overview, 'P21 the overview and the flight back').toBeLessThanOrEqual(budget.longTaskMs)
  expect(hover, 'P21 hovering stars').toBeLessThanOrEqual(budget.longTaskMs)
  expect(focus, 'P21 changing the focus').toBeLessThanOrEqual(budget.longTaskMs)
  expect(hops, 'P21 changing the hops').toBeLessThanOrEqual(budget.longTaskMs)
  expect(path, 'P21 choosing a path').toBeLessThanOrEqual(budget.longTaskMs)
})

// P22, FEAT-070 AC-43: the ring and the particles are the compositor's alone.
test('lineage motion does no layout or paint', async ({ page }) => {
  await serve(page, 'production')
  const address = await serveCapLineage(page)
  await page.goto(address)
  const chart = lineageChart(page)
  await expect(chart.locator('[data-node][data-tier="focus"]')).toHaveCount(1)
  await page.waitForTimeout(1500)
  // On the line itself: an edge is an arc, and its box's centre is off it.
  const onLine = await chart
    .locator('[data-edge]')
    .first()
    .evaluate((element) => {
      const line = element as SVGPathElement
      const point = line.getPointAtLength(line.getTotalLength() / 2)
      const screen = new DOMPoint(point.x, point.y).matrixTransform(
        line.getScreenCTM() ?? undefined,
      )
      return { x: screen.x, y: screen.y }
    })
  await page.mouse.move(onLine.x, onLine.y)
  await expect(page.locator('[data-particle]')).toHaveCount(budget.lineage.particles)
  await page.waitForTimeout(300)

  const moving = (await infiniteAnimations(page)).map(animationName)
  const work = await frameWork(page, budget.quietWindowMs)
  const particles = await page.locator('[data-particle]').count()
  console.warn(
    `P22 layouts ${work.layouts} · paints ${work.paints} over ${budget.quietWindowMs} ms` +
      ` · particles ${particles} · infinite ${moving.length}: ${moving.join(', ')}`,
  )
  expect(moving).toEqual(expect.arrayContaining(['target-ring', 'flow-particle']))
  expect(moving.filter((name) => name === 'flow-particle')).toHaveLength(budget.lineage.particles)
  expect(moving.length, 'P15 infinite animations at once').toBeLessThanOrEqual(
    budget.motion.maxInfiniteAnimations,
  )
  expect(work.layouts, 'P22 Layout events').toBe(0)
  expect(work.paints, 'P22 Paint events').toBe(0)
})

// P3, FEAT-070 AC-44: every script the browser fetches before the lineage page
// draws, measured on the production build rather than read off its manifest.
test('the lineage route loads within its first-load script budget', async ({ page }) => {
  const scripts: Promise<number>[] = []
  page.on('response', (response) => {
    if (response.request().resourceType() === 'script') {
      scripts.push(response.body().then((body) => gzipSync(body, { level: 9 }).length))
    }
  })
  await serve(page)
  await page.goto(await serveCapLineage(page))
  await expect(lineageChart(page).locator('[data-node][data-tier="focus"]')).toHaveCount(1)
  await settleFirstLoad(page)
  const sizes = await Promise.all(scripts)
  const total = sizes.reduce((sum, size) => sum + size, 0)
  console.warn(`P3 lineage route ${(total / 1024).toFixed(1)} kB gzip over ${sizes.length} scripts`)
  expect(sizes.length, 'scripts were measured').toBeGreaterThan(0)
  expect(total, 'P3 first-load script, gzip').toBeLessThanOrEqual(budget.routeKb * 1024)
})

// P3, FEAT-071 AC-30: the observatory's first load, on the tab with the sparklines, which
// are inline SVG so no chart library is in it.
test('the observatory route loads within its first-load script budget', async ({ page }) => {
  const scripts: Promise<number>[] = []
  page.on('response', (response) => {
    if (response.request().resourceType() === 'script') {
      scripts.push(response.body().then((body) => gzipSync(body, { level: 9 }).length))
    }
  })
  await serve(page)
  await serveObservatory(page)
  await page.goto('/observatory?tab=capacity')
  await expect(page.locator('svg.sparkline').first()).toBeVisible()
  await settleFirstLoad(page)
  const sizes = await Promise.all(scripts)
  const total = sizes.reduce((sum, size) => sum + size, 0)
  console.warn(
    `P3 observatory route ${(total / 1024).toFixed(1)} kB gzip over ${sizes.length} scripts`,
  )
  expect(sizes.length, 'scripts were measured').toBeGreaterThan(0)
  expect(total, 'P3 first-load script, gzip').toBeLessThanOrEqual(budget.routeKb * 1024)
})

// P3 and P11, FEAT-072 AC-13: the overview is the observatory's landing view, so its
// first load is the route's, and neither opening it nor reading its charts blocks the
// main thread.
test('the observatory overview loads within budget and reading it produces no long task', async ({
  page,
}) => {
  const scripts: Promise<number>[] = []
  page.on('response', (response) => {
    if (response.request().resourceType() === 'script') {
      scripts.push(response.body().then((body) => gzipSync(body, { level: 9 }).length))
    }
  })
  await serve(page)
  await serveObservatory(page)
  await page.goto('/observatory')
  await expect(page.locator('[data-chart="volume"] [data-testid="chart-plot"]')).toBeVisible()
  await settleFirstLoad(page)
  const sizes = await Promise.all(scripts)
  const total = sizes.reduce((sum, size) => sum + size, 0)

  await page.goto('about:blank')
  await observe(page)
  await throttle(page)
  await page.goto('/observatory?tab=capacity')
  await expect(page.locator('svg.sparkline').first()).toBeVisible()
  await page.waitForLoadState('networkidle')
  await nextPaint(page)
  const measure = async (work: () => Promise<void>) => {
    const before = await taskCount(page)
    await work()
    await page.waitForLoadState('networkidle')
    await nextPaint(page)
    await page.waitForTimeout(450)
    return longestSince(page, before)
  }
  const opening = await measure(async () => {
    await page.locator('[role="tab"]', { hasText: 'Overview' }).click()
    await expect(page.locator('[data-chart="latency"] [data-testid="chart-plot"]')).toBeVisible()
  })
  const reading = await measure(async () => {
    for (const name of ['volume', 'latency']) {
      const box = await page
        .locator(`[data-chart="${name}"] [data-testid="chart-plot"]`)
        .boundingBox()
      if (box === null) throw new Error(`the ${name} chart has no box`)
      for (const share of [0.1, 0.3, 0.5, 0.7, 0.9]) {
        await page.mouse.move(box.x + box.width * share, box.y + box.height / 2)
      }
    }
    await page.mouse.move(5, 895)
  })
  console.warn(
    `P3 observatory overview ${(total / 1024).toFixed(1)} kB gzip over ${sizes.length} scripts` +
      ` · P11 opening ${opening} ms · reading ${reading} ms`,
  )
  expect(sizes.length, 'scripts were measured').toBeGreaterThan(0)
  expect(total, 'P3 first-load script, gzip').toBeLessThanOrEqual(budget.routeKb * 1024)
  expect(opening, 'P11 opening the overview').toBeLessThanOrEqual(budget.longTaskMs)
  expect(reading, 'P11 reading the charts').toBeLessThanOrEqual(budget.longTaskMs)
})

// P3 and P11, FEAT-073 AC-13: the routine timing tab is a drill-down, so it stays out of
// the overview's first load, and opening it from the overview and sorting it block nothing.
test('FEAT-073 AC-13: the routine timing tab opens and sorts without a long task, and the overview stays within its budget', async ({
  page,
}) => {
  const scripts: Promise<number>[] = []
  page.on('response', (response) => {
    if (response.request().resourceType() === 'script') {
      scripts.push(response.body().then((body) => gzipSync(body, { level: 9 }).length))
    }
  })
  await serve(page)
  await serveObservatory(page)
  await page.goto('/observatory')
  await expect(page.locator('[data-chart="volume"] [data-testid="chart-plot"]')).toBeVisible()
  await settleFirstLoad(page)
  const sizes = await Promise.all(scripts)
  const total = sizes.reduce((sum, size) => sum + size, 0)

  await page.goto('about:blank')
  await observe(page)
  await throttle(page)
  await page.goto('/observatory')
  await expect(page.locator('[data-chart="latency"] [data-testid="chart-plot"]')).toBeVisible()
  await page.waitForLoadState('networkidle')
  await nextPaint(page)
  const rows = page.locator('[data-testid="routines-table"] tr[data-routine-row]')
  const measure = async (work: () => Promise<void>) => {
    const before = await taskCount(page)
    await work()
    await page.waitForLoadState('networkidle')
    await nextPaint(page)
    await page.waitForTimeout(450)
    return longestSince(page, before)
  }
  const opening = await measure(async () => {
    await page.locator('[role="tab"]', { hasText: 'Routine timing' }).click()
    await expect(rows.first()).toBeVisible()
  })
  const sorting = await measure(async () => {
    await page.locator('th[data-sort="total"] button').click()
    await expect(rows.first()).toContainText('settle_round')
  })
  console.warn(
    `P3 observatory overview ${(total / 1024).toFixed(1)} kB gzip over ${sizes.length} scripts` +
      ` · P11 opening routine timing ${opening} ms · sorting ${sorting} ms`,
  )
  expect(sizes.length, 'scripts were measured').toBeGreaterThan(0)
  expect(total, 'P3 first-load script, gzip').toBeLessThanOrEqual(budget.routeKb * 1024)
  expect(opening, 'P11 opening routine timing').toBeLessThanOrEqual(budget.longTaskMs)
  expect(sorting, 'P11 sorting routine timing').toBeLessThanOrEqual(budget.longTaskMs)
})

// The gates are worth nothing if they cannot fail. These are the two
// detectors above, pointed at things they must catch.
test('tells a blocked main thread and a painting animation from quiet ones', async ({ page }) => {
  await serve(page, 'development', false)
  await observe(page)
  await page.goto('/login')
  await page.getByRole('button', { name: 'Sign in' }).waitFor()

  const before = await taskCount(page)
  await page.evaluate(() => {
    setTimeout(() => {
      const until = performance.now() + 120
      while (performance.now() < until) {
        // The probe is the busy loop itself.
      }
    }, 0)
  })
  await page.waitForTimeout(400)
  expect(await longestSince(page, before)).toBeGreaterThan(budget.longTaskMs)

  const quiet = await frameWork(page, 1000)
  expect(quiet.paints).toBe(0)
  await page.evaluate(() => {
    const probe = document.createElement('div')
    probe.style.cssText = 'position:fixed;top:0;left:0;width:40px;height:40px'
    document.body.append(probe)
    probe.animate([{ boxShadow: '0 0 0 0 red' }, { boxShadow: '0 0 0 12px red' }], {
      duration: 500,
      iterations: Infinity,
    })
  })
  const painted = await frameWork(page, 1000)
  expect(painted.paints).toBeGreaterThan(10)

  // A brush whose handler blocks for 80 ms is a long task the sky case catches.
  await page.unrouteAll({ behavior: 'ignoreErrors' })
  await serve(page)
  await page.goto('/audit/all')
  await page.getByTestId('audit-sky').locator('svg').waitFor()
  await page.waitForLoadState('networkidle')
  await page.evaluate(() => {
    document.querySelector('[data-testid="audit-sky"] svg')?.addEventListener(
      'pointermove',
      () => {
        const until = performance.now() + 80
        while (performance.now() < until) {
          // The probe is the blocked handler itself.
        }
      },
      { once: true },
    )
  })
  const beforeBrush = await taskCount(page)
  await brushSky(page)
  await page.waitForTimeout(200)
  expect(await longestSince(page, beforeBrush)).toBeGreaterThan(budget.longTaskMs)
})
