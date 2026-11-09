import { describe, expect, it } from 'vitest'

import {
  areaPath,
  barFractions,
  barTotal,
  describeBars,
  describeSeries,
  frame,
  isBlank,
  logTicks,
  positiveRange,
  linePath,
  nearestIndex,
  niceStep,
  niceTicks,
  plotHeight,
  plotWidth,
  spreadIndices,
  stack,
  toChartX,
  valueRange,
  xScale,
  xTicks,
  yScale,
  type Box,
} from './geometry'
import { readingsAt, timeLayout } from './timeLayout'
import type { Series } from './types'

const box: Box = { width: 368, height: 130, frame }

function series(id: string, ys: (number | null)[]): Series {
  return {
    id,
    label: id,
    tone: 'primary',
    points: ys.map((y, index) => ({ x: index * 1000, y })),
  }
}

const fmt = (value: number) => `${value}`

describe('the plot area', () => {
  it('is the box less its frame, never less than a pixel', () => {
    expect(plotWidth(box)).toBe(300)
    expect(plotHeight(box)).toBe(100)
    expect(plotWidth({ ...box, width: 10 })).toBe(1)
  })
})

describe('scales', () => {
  it('maps the first and last x to the plot edges', () => {
    const x = xScale([0, 500, 1000], box)
    expect(x.toPixel(0)).toBe(56)
    expect(x.toPixel(500)).toBe(206)
    expect(x.toPixel(1000)).toBe(356)
  })

  it('centres a single x', () => {
    expect(xScale([42], box).toPixel(42)).toBe(206)
  })

  it('puts the lowest tick on the floor and the highest on the ceiling', () => {
    const y = yScale([0, 50, 100], box)
    expect(y.toPixel(0)).toBe(108)
    expect(y.toPixel(100)).toBe(8)
    expect(y.toPixel(50)).toBe(58)
  })

  it('reads a pointer back into chart coordinates at any shown width', () => {
    expect(toChartX(150, 50, 200, 400)).toBe(200)
    expect(toChartX(150, 50, 0, 400)).toBe(100)
  })
})

describe('ticks', () => {
  it('steps by 1, 2, 2.5 or 5 of a power of ten', () => {
    expect(niceStep(0.7)).toBe(1)
    expect(niceStep(1.5)).toBe(2)
    expect(niceStep(2.2)).toBe(2.5)
    expect(niceStep(3)).toBe(5)
    expect(niceStep(7)).toBe(10)
    expect(niceStep(340)).toBe(500)
    expect(niceStep(0)).toBe(1)
    expect(niceStep(Number.NaN)).toBe(1)
  })

  it('covers the range in round numbers', () => {
    expect(niceTicks(0, 87, 3)).toEqual([0, 50, 100])
    expect(niceTicks(0, 1200, 3)).toEqual([0, 500, 1000, 1500])
    expect(niceTicks(-10, 10, 2)).toEqual([-10, 0, 10])
  })

  it('still draws an axis when every value is the same', () => {
    expect(niceTicks(0, 0)).toEqual([0, 1])
    expect(niceTicks(5, 5)).toEqual([0, 5])
    expect(niceTicks(-3, -3)).toEqual([-3, 1])
  })

  it('spreads x ticks from the first bucket to the last', () => {
    expect(spreadIndices(0, 4)).toEqual([])
    expect(spreadIndices(1, 4)).toEqual([0])
    expect(spreadIndices(10, 4)).toEqual([0, 3, 6, 9])
    expect(spreadIndices(3, 8)).toEqual([0, 1, 2])
    const ticks = xTicks([0, 1000, 2000], xScale([0, 1000, 2000], box), box, (x) => `t${x}`)
    expect(ticks.map((tick) => tick.label)).toEqual(['t0', 't2000'])
  })
})

describe('stacking', () => {
  it('rests each series on the ones below it, a null counting as zero', () => {
    const bands = stack([series('a', [1, null, 3]), series('b', [2, 2, null])])
    expect(bands[0]).toEqual({ low: [0, 0, 0], high: [1, 0, 3] })
    expect(bands[1]).toEqual({ low: [1, 0, 3], high: [3, 2, 3] })
  })

  it('ranges over the stack top when stacked, over the values when not', () => {
    const both = [series('a', [1, 4]), series('b', [5, -2])]
    expect(valueRange(both, 'stacked-area')).toEqual([0, 6])
    expect(valueRange(both, 'line')).toEqual([-2, 5])
    expect(valueRange([], 'stacked-area')).toEqual([0, 0])
  })
})

describe('paths', () => {
  const toY = (value: number) => 100 - value

  it('breaks a line at a null and dots a value with no neighbour', () => {
    expect(linePath([0, 10, 20, 30, 40], [1, 2, null, 4, null], toY)).toBe('M0 99L10 98M30 96l0 0')
    expect(linePath([0, 10], [null, null], toY)).toBe('')
  })

  it('outlines a band along its ceiling and back along its floor', () => {
    expect(areaPath([0, 10], { low: [0, 1], high: [2, 3] }, toY)).toBe('M0 98L10 97L10 99L0 100Z')
    expect(areaPath([], { low: [], high: [] }, toY)).toBe('')
  })
})

describe('the nearest bucket', () => {
  const xs = [0, 10, 20, 30]

  it('is found by bisection, ties going left', () => {
    expect(nearestIndex(xs, -5)).toBe(0)
    expect(nearestIndex(xs, 14)).toBe(1)
    expect(nearestIndex(xs, 15)).toBe(1)
    expect(nearestIndex(xs, 16)).toBe(2)
    expect(nearestIndex(xs, 99)).toBe(3)
    expect(nearestIndex([7], 0)).toBe(0)
    expect(nearestIndex([], 0)).toBe(-1)
  })

  it('stays fast at the largest window', () => {
    const many = Array.from({ length: 744 }, (_, index) => index * 3600_000)
    expect(nearestIndex(many, 400.4 * 3600_000)).toBe(400)
  })
})

describe('blank and summary', () => {
  it('calls a chart blank when there is nothing but nulls and zeros', () => {
    expect(isBlank([])).toBe(true)
    expect(isBlank([series('a', [0, null])])).toBe(true)
    expect(isBlank([series('a', [0, 1])])).toBe(false)
  })

  it('says each series low, high and latest in words', () => {
    const text = describeSeries([series('p95', [3, null, 9, 5]), series('p50', [null])], fmt, fmt)
    expect(text).toBe('From 0 to 3000. p95: low 3, high 9, latest 5. p50: no values.')
    expect(describeSeries([], fmt, fmt)).toBe('No data.')
  })
})

describe('the time layout', () => {
  it('draws one line per series and the areas only when stacked', () => {
    const data = [series('a', [1, 2]), series('b', [3, 4])]
    const lines = timeLayout(data, 'line', box, { formatY: fmt, formatX: fmt })
    expect(lines.drawn.map((one) => one.area)).toEqual([null, null])
    expect(lines.pixelsX).toEqual([56, 356])
    const stacked = timeLayout(data, 'stacked-area', box, { formatY: fmt, formatX: fmt })
    expect(stacked.drawn.every((one) => one.area?.startsWith('M'))).toBe(true)
    expect(stacked.yTicks.map((tick) => tick.label)).toEqual(['0', '2', '4', '6'])
  })

  it('reads a bucket raw, not stacked, and a stacked gap as zero', () => {
    const data = [series('a', [1, null]), series('b', [3, 4])]
    expect(readingsAt(data, 'stacked-area', 1, fmt).map((r) => r.text)).toEqual(['0', '4'])
    expect(readingsAt(data, 'line', 1, fmt).map((r) => r.text)).toEqual(['no value', '4'])
  })

  it('lays out 12 series of 744 buckets well inside a frame', () => {
    const data = Array.from({ length: 12 }, (_, s) =>
      series(
        `s${s}`,
        Array.from({ length: 744 }, (_, i) => (i * (s + 1)) % 97),
      ),
    )
    // The best of five: a single cold call under coverage instrumentation on
    // a shared runner measured the JIT and the neighbours, and failed CI at 63 ms.
    const runs = Array.from({ length: 5 }, () => {
      const started = performance.now()
      timeLayout(data, 'stacked-area', { ...box, width: 1200 }, { formatY: fmt, formatX: fmt })
      return performance.now() - started
    })
    expect(Math.min(...runs)).toBeLessThan(50)
  })
})

describe('bars', () => {
  const segmented = {
    id: 'b',
    label: 'b',
    value: 0,
    segments: [
      { id: 't', label: 'tables', tone: 'primary' as const, value: 15 },
      { id: 'v', label: 'views', tone: 'series' as const, value: 5 },
    ],
  }
  const bars = [{ id: 'a', label: 'a', value: 10 }, segmented]

  it('sums segments into the bar and scales to the longest', () => {
    expect(barTotal(segmented)).toBe(20)
    expect(barFractions(bars)).toEqual([0.5, 1])
    expect(barFractions([{ id: 'z', label: 'z', value: 0 }])).toEqual([0])
  })

  it('names the largest in words', () => {
    expect(describeBars(bars, fmt)).toBe('2 bars; the largest is b at 20.')
    expect(describeBars([{ id: 'a', label: 'a', value: 1 }], fmt)).toBe(
      '1 bar; the largest is a at 1.',
    )
    expect(describeBars([], fmt)).toBe('No data.')
  })
})

describe('a logarithmic latency axis', () => {
  const lines: Series[] = [
    {
      id: 'p50',
      label: 'p50',
      tone: 'primary',
      points: [30, 25, null, 40].map((y, x) => ({ x, y })),
    },
    {
      id: 'p95',
      label: 'p95',
      tone: 'warn',
      points: [900, 1200, null, 26_000].map((y, x) => ({ x, y })),
    },
  ]
  const fmt = (value: number) => String(value)

  it('covers the positive values with powers of ten', () => {
    expect(positiveRange(lines)).toEqual([25, 26_000])
    expect(logTicks(25, 26_000)).toEqual([10, 100, 1000, 10_000, 100_000])
    expect(logTicks(5, 5)).toEqual([1, 10])
    expect(positiveRange([{ id: 'z', label: 'z', tone: 'muted', points: [{ x: 0, y: 0 }] }])).toBe(
      null,
    )
  })

  it('keeps p50 well clear of the floor when one p95 is an outlier', () => {
    const layout = timeLayout(lines, 'line', box, { formatY: fmt, formatX: fmt }, 'log')
    expect(layout.logarithmic).toBe(true)
    const floor = box.height - box.frame.bottom
    const top = box.frame.top
    const height = floor - top
    const p50 = layout.drawn[0]?.tops[0] ?? floor
    const p95 = layout.drawn[1]?.tops[0] ?? floor
    expect((floor - p50) / height).toBeGreaterThan(0.1)
    expect((p50 - p95) / height).toBeGreaterThan(0.25)
    expect(layout.yTicks.map((tick) => tick.label)).toEqual([
      '10',
      '100',
      '1000',
      '10000',
      '100000',
    ])
    expect(readingsAt(lines, 'line', 3, fmt)[1]?.text).toBe('26000')
  })

  it('stays linear unless asked, and for stacked areas', () => {
    expect(timeLayout(lines, 'line', box, { formatY: fmt, formatX: fmt }).logarithmic).toBe(false)
    expect(
      timeLayout(lines, 'stacked-area', box, { formatY: fmt, formatX: fmt }, 'log').logarithmic,
    ).toBe(false)
  })
})
