import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VSparkline from './VSparkline.vue'
import { polyline, sparkHeight, sparkPoints, sparkWidth, trend } from './sparkline'

describe('sparkPoints', () => {
  it('spreads the values across the width, lowest on the floor and highest on the ceiling', () => {
    const points = sparkPoints([10, 30, 20])
    expect(points.map((point) => point.x)).toEqual([0, sparkWidth / 2, sparkWidth])
    expect(points[0]?.y).toBe(sparkHeight - 2)
    expect(points[1]?.y).toBe(2)
    expect(points[2]?.y).toBe(sparkHeight / 2)
  })

  it('draws a flat series through the middle, not along the floor', () => {
    expect(sparkPoints([5, 5, 5]).every((point) => point.y === sparkHeight / 2)).toBe(true)
  })

  it('centres a single value and draws nothing for none', () => {
    expect(sparkPoints([7])).toEqual([{ x: sparkWidth / 2, y: sparkHeight / 2 }])
    expect(sparkPoints([])).toEqual([])
  })

  it('writes the points as an SVG list', () => {
    expect(
      polyline([
        { x: 0, y: 1 },
        { x: 2.5, y: 3 },
      ]),
    ).toBe('0,1 2.5,3')
  })
})

describe('trend', () => {
  it('reads the first and last value', () => {
    expect(trend([1, 9, 2])).toBe('rising')
    expect(trend([5, 1])).toBe('falling')
    expect(trend([3, 8, 3])).toBe('flat')
    expect(trend([4])).toBe('flat')
    expect(trend([])).toBe('empty')
  })
})

describe('VSparkline', () => {
  it('draws one line and a mark on the latest value, labelled with its trend', () => {
    const line = mount(VSparkline, { props: { values: [1, 2, 4], label: 'aviorfixture size' } })
    expect(line.find('polyline').attributes('points')).toBe(polyline(sparkPoints([1, 2, 4])))
    expect(line.find('circle').attributes('cx')).toBe(String(sparkWidth))
    expect(line.find('svg').attributes('aria-label')).toBe('aviorfixture size: rising')
    expect(line.find('svg').attributes('data-trend')).toBe('rising')
    expect(line.find('title').text()).toBe('aviorfixture size: rising')
  })

  it('says there is no history rather than drawing an empty box', () => {
    const empty = mount(VSparkline, { props: { values: [], label: 'ledger size' } })
    expect(empty.find('polyline').exists()).toBe(false)
    expect(empty.find('circle').exists()).toBe(false)
    expect(empty.find('line').attributes('stroke-dasharray')).toBe('2 3')
    expect(empty.find('svg').attributes('aria-label')).toBe('ledger size: no history yet')
  })
})
