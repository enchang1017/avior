import { describe, expect, it } from 'vitest'

import { sceneHeight, sceneWidth } from '../VLineageChart/types'
import { galaxyLayout, labelBox, radiusOf } from './galaxy'

const schemas = Array.from({ length: 24 }, (_, index) => ({
  schema: `schema_${index}`,
  objects: (index % 7) * 40 + 3,
}))

describe('galaxyLayout', () => {
  it('sizes a nebula by its object count, by area', () => {
    expect(radiusOf(400, 400)).toBeGreaterThan(radiusOf(100, 400))
    expect(radiusOf(400, 400) / radiusOf(100, 400)).toBeLessThan(2)
  })

  it('puts the largest schema at the centre and keeps every nebula inside the scene and apart', () => {
    const { clusters } = galaxyLayout(schemas, [])
    const largest = [...clusters].sort((a, b) => b.objects - a.objects)[0]
    expect([largest?.x, largest?.y]).toEqual([0, 0])
    for (const one of clusters) {
      expect(Math.abs(one.x) + one.r).toBeLessThanOrEqual(sceneWidth / 2)
      expect(Math.abs(one.y) + one.r).toBeLessThanOrEqual(sceneHeight / 2)
    }
    for (const [index, a] of clusters.entries())
      for (const b of clusters.slice(index + 1))
        expect(Math.hypot(a.x - b.x, a.y - b.y), `${a.schema} × ${b.schema}`).toBeGreaterThan(
          a.r + b.r,
        )
  })

  it('draws a link between two schemas as thick as it is many, and none to a schema not drawn', () => {
    const { links } = galaxyLayout(schemas.slice(0, 3), [
      { from: 'schema_0', to: 'schema_1', edges: 1 },
      { from: 'schema_1', to: 'schema_2', edges: 16 },
      { from: 'schema_1', to: 'elsewhere', edges: 3 },
    ])
    expect(links.map((link) => link.key)).toEqual(['schema_0|schema_1', 'schema_1|schema_2'])
    expect(links[1]?.width).toBeGreaterThan(links[0]?.width ?? 0)
  })

  it('grows a small galaxy to fill the view', () => {
    const { clusters } = galaxyLayout(schemas.slice(0, 2), [])
    const reach = Math.max(...clusters.map((one) => Math.abs(one.x) + one.r, 0))
    const tall = Math.max(...clusters.map((one) => Math.abs(one.y) + one.r, 0))
    expect(Math.max(reach / (sceneWidth / 2), tall / (sceneHeight / 2))).toBeGreaterThan(0.5)
  })

  it('ends each link at the two nebulae and never runs it through a schema name', () => {
    const few = schemas.slice(0, 8)
    const links = few.flatMap((a, i) =>
      few.slice(i + 1).map((b) => ({ from: a.schema, to: b.schema, edges: 2 })),
    )
    const layout = galaxyLayout(few, links)
    const at = new Map(layout.clusters.map((one) => [one.schema, one]))
    for (const link of layout.links) {
      const [from, to] = link.key.split('|').map((name) => at.get(name ?? ''))
      expect(Math.hypot(link.x1 - (from?.x ?? 0), link.y1 - (from?.y ?? 0))).toBeCloseTo(
        from?.r ?? 0,
        0,
      )
      expect(Math.hypot(link.x2 - (to?.x ?? 0), link.y2 - (to?.y ?? 0))).toBeCloseTo(to?.r ?? 0, 0)
    }
    for (const cluster of layout.clusters) {
      const box = labelBox(cluster.schema, cluster.label)
      for (const link of layout.links.filter((one) =>
        one.key.split('|').includes(cluster.schema),
      )) {
        for (let step = 0; step <= 50; step += 1) {
          const x = link.x1 + ((link.x2 - link.x1) * step) / 50
          const y = link.y1 + ((link.y2 - link.y1) * step) / 50
          const inside = x > box.left && x < box.right && y > box.top && y < box.bottom
          expect(inside, `${link.key} crosses the name ${cluster.schema}`).toBe(false)
        }
      }
    }
  })

  it('draws the same galaxy whatever order the schemas come in', () => {
    expect(galaxyLayout([...schemas].reverse(), [])).toEqual(galaxyLayout(schemas, []))
  })
})
