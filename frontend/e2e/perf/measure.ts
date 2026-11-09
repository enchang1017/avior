import type { CDPSession, Page } from '@playwright/test'

/** ADR 0009's measurement conditions: 4x CPU, a cold cache, 1440x900. */
export const cpuThrottle = 4

declare global {
  interface Window {
    __perf: {
      longTasks: number[]
      lcp: number
      cls: number
      marks: Record<string, number>
    }
  }
}

export async function throttle(page: Page, rate = cpuThrottle): Promise<CDPSession> {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Emulation.setCPUThrottlingRate', { rate })
  return cdp
}

export async function observe(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const perf = { longTasks: [] as number[], lcp: 0, cls: 0, marks: {} as Record<string, number> }
    window.__perf = perf
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) perf.longTasks.push(entry.duration)
    }).observe({ type: 'longtask', buffered: true })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) perf.lcp = entry.startTime
    }).observe({ type: 'largest-contentful-paint', buffered: true })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries() as (PerformanceEntry & {
        value: number
        hadRecentInput: boolean
      })[]) {
        if (!entry.hadRecentInput) perf.cls += entry.value
      }
    }).observe({ type: 'layout-shift', buffered: true })
  })
}

export async function taskCount(page: Page): Promise<number> {
  return page.evaluate(() => window.__perf.longTasks.length)
}

export async function longestSince(page: Page, since: number): Promise<number> {
  const all = await page.evaluate(() => window.__perf.longTasks)
  return Math.round(Math.max(0, ...all.slice(since)))
}

export async function vitals(page: Page): Promise<{ lcp: number; cls: number }> {
  await page.waitForFunction(() => window.__perf.lcp > 0)
  return page.evaluate(() => ({
    lcp: Math.round(window.__perf.lcp),
    cls: Math.round(window.__perf.cls * 1000) / 1000,
  }))
}

export type FrameWork = { layouts: number; paints: number }

/** Layout and Paint events the renderer's main thread records over a window. */
/**
 * Waits until a window of windowMs cannot contain a wall-clock minute. The HUD's
 * UTC clock rewrites its text on the minute, which is one Layout and a Paint
 * that are the clock's and not the motion's being measured, and a 5 s window
 * spans a minute about one run in twelve.
 */
async function clearOfTheMinute(page: Page, windowMs: number): Promise<void> {
  const margin = 1000
  const intoMinute = await page.evaluate(() => Date.now() % 60_000)
  if (intoMinute + windowMs + margin > 60_000) {
    await page.waitForTimeout(60_000 - intoMinute + margin)
  }
}

export async function frameWork(page: Page, windowMs: number): Promise<FrameWork> {
  await clearOfTheMinute(page, windowMs)
  const cdp = await page.context().newCDPSession(page)
  const names: string[] = []
  cdp.on('Tracing.dataCollected', (data) => {
    for (const event of data.value) names.push(event['name'] ?? '')
  })
  const done = new Promise<void>((resolve) => cdp.once('Tracing.tracingComplete', () => resolve()))
  await cdp.send('Tracing.start', {
    traceConfig: {
      includedCategories: ['devtools.timeline', 'disabled-by-default-devtools.timeline'],
    },
    transferMode: 'ReportEvents',
  })
  await page.waitForTimeout(windowMs)
  await cdp.send('Tracing.end')
  await done
  await cdp.detach()
  return {
    layouts: names.filter((name) => name === 'Layout').length,
    paints: names.filter((name) => name === 'Paint').length,
  }
}

/** The share of frame intervals within a limit, sampled by requestAnimationFrame. */
export async function smoothFrames(page: Page, windowMs: number, limitMs: number): Promise<number> {
  return page.evaluate(
    ({ windowMs, limitMs }) =>
      new Promise<number>((resolve) => {
        const stamps: number[] = []
        const start = performance.now()
        const tick = (now: number) => {
          stamps.push(now)
          if (now - start < windowMs) requestAnimationFrame(tick)
          else {
            const gaps = stamps.slice(1).map((stamp, index) => stamp - (stamps[index] ?? stamp))
            resolve(gaps.filter((gap) => gap <= limitMs).length / Math.max(1, gaps.length))
          }
        }
        requestAnimationFrame(tick)
      }),
    { windowMs, limitMs },
  )
}

/** Infinite animations running now, by name: what P15 counts. */
export async function infiniteAnimations(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    document
      .getAnimations()
      .filter(
        (animation) =>
          animation.playState === 'running' &&
          animation.effect?.getComputedTiming().iterations === Infinity,
      )
      .map((animation) => (animation instanceof CSSAnimation ? animation.animationName : 'script')),
  )
}

/** Resolves on the frame after the next one: the moment a change is on screen. */
export async function nextPaint(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  )
}

/**
 * Waits until the page has fetched everything it fetches once it has drawn:
 * an idle callback, queued after the shell's own idle preloads (the palette),
 * and then a quiet network. P3 sums the scripts after this, so it measures the
 * heavier first load every time instead of whichever one won a race with the
 * idle preloads.
 */
export async function settleFirstLoad(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        if ('requestIdleCallback' in window) requestIdleCallback(() => resolve(), { timeout: 2000 })
        else setTimeout(resolve, 0)
      }),
  )
  await page.waitForLoadState('networkidle')
}
