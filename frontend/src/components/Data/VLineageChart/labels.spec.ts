import { describe, expect, it } from 'vitest'

import { placeLabels, type Box, type LabelInput, type LabelMetrics } from './labels'
import { sceneHeight, sceneWidth } from './types'

const metrics: LabelMetrics = { charWidth: 7, lineHeight: 14, ringRadius: 18, gap: 10, pad: 4 }

const star = (id: string, x: number, y: number, extra: Partial<LabelInput> = {}): LabelInput => ({
  id,
  x,
  y,
  tier: 'near',
  labelled: true,
  text: id,
  badge: null,
  ...extra,
})

const inside = (box: Box) =>
  box.left >= -sceneWidth / 2 &&
  box.right <= sceneWidth / 2 &&
  box.top >= -sceneHeight / 2 &&
  box.bottom <= sceneHeight / 2

const crosses = (a: Box, b: Box) =>
  a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom

/** A crowded ring, the way two hops of a busy table come back. */
function ring(count: number, rx: number, ry: number): LabelInput[] {
  return Array.from({ length: count }, (_, index) => {
    const angle = (2 * Math.PI * index) / count
    const name = `routine_${index}(p_argument_${index} integer, p_other numeric)`
    return star(name, Math.cos(angle) * rx, Math.sin(angle) * ry, {
      badge: index % 5 === 0 ? 'DYNAMIC' : null,
    })
  })
}

describe('placeLabels', () => {
  it('puts a label on the side away from the centre, and the focus below its ring', () => {
    const labels = placeLabels(
      [star('round', 0, 0, { tier: 'focus' }), star('wallet', -200, 40), star('player', 200, -40)],
      metrics,
    )
    const of = (id: string) => labels.find((label) => label.id === id)
    expect(of('wallet')?.anchor).toBe('end')
    expect(of('player')?.anchor).toBe('start')
    expect(of('round')?.anchor).toBe('middle')
    expect(of('round')?.box.top).toBeGreaterThanOrEqual(metrics.ringRadius)
  })

  it('flips a label near the edge to the inner side of its star rather than cut it', () => {
    const name = 'adjustment_adjustment_id_seq'
    const [label] = placeLabels([star(name, sceneWidth / 2 - 60, 0)], metrics)
    expect(label?.anchor).toBe('end')
    expect(label?.lines[0]).toBe(name)
    expect(label !== undefined && label.box.right).toBeLessThanOrEqual(sceneWidth / 2 - 60)
    expect(label !== undefined && inside(label.box)).toBe(true)
  })

  it('keeps a label outward when it fits there', () => {
    const [label] = placeLabels([star('bet', sceneWidth / 2 - 60, 0)], metrics)
    expect(label?.anchor).toBe('start')
  })

  it('cuts a name that fits on neither side, and keeps it whole for the tooltip', () => {
    const name = `archive_table(${'p_argument text, '.repeat(8)}p_dry_run boolean)`
    const [label] = placeLabels([star(name, -sceneWidth / 2 + 80, 0)], metrics)
    expect(label?.lines[0]?.endsWith('…')).toBe(true)
    expect(label?.full).toBe(name)
    expect(label !== undefined && inside(label.box)).toBe(true)
  })

  it('draws the badge as a line of the same block, so it moves with the name', () => {
    const [label] = placeLabels([star('archive_table', 150, 0, { badge: 'DYNAMIC' })], metrics)
    expect(label?.lines).toEqual(['archive_table', 'DYNAMIC'])
    expect(label?.badgeLine).toBe(1)
    expect(label === undefined ? 0 : label.box.bottom - label.box.top).toBe(2 * metrics.lineHeight)
  })

  it('keeps every label inside the scene and no two crossing on a crowded ring', () => {
    const labels = placeLabels(ring(40, 330, 280), metrics)
    expect(labels).toHaveLength(40)
    for (const label of labels) expect(inside(label.box), label.id).toBe(true)
    for (const [index, a] of labels.entries())
      for (const b of labels.slice(index + 1))
        expect(crosses(a.box, b.box), `${a.id} × ${b.id}`).toBe(false)
  })

  it('places the same scene the same way every time', () => {
    const stars = ring(30, 300, 250)
    expect(placeLabels([...stars].reverse(), metrics)).toEqual(placeLabels(stars, metrics))
  })

  it('labels a hundred and fifty stars well inside the layout budget', () => {
    const stars = ring(150, 330, 290)
    placeLabels(stars, metrics)
    // The best of five after a warm run: a suite running in parallel stalls any
    // one sample, and what this guards against is a placement that grows too fast.
    const runs = Array.from({ length: 5 }, () => {
      const started = performance.now()
      placeLabels(stars, metrics)
      return performance.now() - started
    })
    expect(Math.min(...runs)).toBeLessThan(16)
  })
})
