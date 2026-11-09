import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VSchemaChart from './VSchemaChart.vue'
import type { SchemaGraph } from './graph'

const graph: SchemaGraph = {
  nodes: [
    { schema: 'public', name: 'member', kind: 'table', columns: 4, approxRows: 1_250_000 },
    { schema: 'public', name: 'bet', kind: 'table', columns: 6, approxRows: null },
    { schema: 'public', name: 'member_summary', kind: 'view', columns: 3, approxRows: null },
  ],
  edges: [
    {
      from: { schema: 'public', name: 'bet' },
      to: { schema: 'public', name: 'member' },
      kind: 'foreign_key',
    },
    {
      from: { schema: 'public', name: 'member_summary' },
      to: { schema: 'public', name: 'member' },
      kind: 'view_dependency',
    },
  ],
  total: { nodes: 3, edges: 2 },
  truncated: false,
}

const render = (
  target: { schema: string; name: string } | null = { schema: 'public', name: 'member' },
) => mount(VSchemaChart, { props: { graph, database: 'aviorfixture', target } })

describe('VSchemaChart', () => {
  it('draws one star per object, tables filled and views hollow, the target in amber', () => {
    const chart = render()
    const stars = chart.findAll('circle.star')
    expect(stars).toHaveLength(3)
    expect(chart.findAll('[data-node]')).toHaveLength(3)
    expect(chart.findAll('[data-edge]')).toHaveLength(2)
    const centre = chart.find('circle[data-ring="0"]')
    expect(centre.attributes('data-key')).toBe('public.member')
    expect(centre.classes()).toContain('fill-target')
    expect(chart.find('circle[data-kind="view"]').classes()).toContain('fill-surface')
    expect(chart.findAll('line')).toHaveLength(2)
    expect(chart.text()).toContain('1.25M · 4 col')
  })

  it('describes itself for a screen reader', () => {
    expect(render().find('svg').attributes('aria-label')).toBe(
      'aviorfixture: 3 objects, 2 relationships. member references nothing and is referenced by bet and member_summary.',
    )
  })

  it('chooses the object whose star is clicked', async () => {
    const chart = render()
    await chart.find('circle[data-key="public.bet"]').trigger('click')
    expect(chart.emitted('choose')).toEqual([[{ schema: 'public', name: 'bet', kind: 'table' }]])
  })

  it('ignores a click between the stars', async () => {
    const chart = render()
    await chart.find('g.cursor-pointer').trigger('click')
    expect(chart.emitted('choose')).toBeUndefined()
  })

  it('names an unlabelled star while the pointer is over it', async () => {
    const nodes = Array.from({ length: 60 }, (_, i) => ({
      schema: 'public',
      name: `t${i}`,
      kind: 'table' as const,
      columns: 1,
      approxRows: null,
    }))
    const chart = mount(VSchemaChart, {
      props: {
        graph: { nodes, edges: [], total: { nodes: 60, edges: 0 }, truncated: false },
        database: 'x',
        target: null,
      },
    })
    await chart.find('circle[data-key="public.t7"]').trigger('pointerover')
    expect(chart.text()).toContain('public.t7')
    await chart.find('g.cursor-pointer').trigger('pointerleave')
    expect(chart.text()).not.toContain('public.t7')
  })

  it('says how many it shows of how many', () => {
    const big = { ...graph, total: { nodes: 612, edges: 2 } }
    const chart = mount(VSchemaChart, { props: { graph: big, database: 'x', target: null } })
    expect(chart.find('.showing').text()).toContain('showing 3 of 612')
  })

  it('lights a star and its neighbours while the pointer is on it, and dims the rest', async () => {
    const chart = render()
    await chart.find('circle[data-key="public.bet"]').trigger('pointerover')
    const lit = chart
      .findAll('g.node.lit')
      .map((node) => node.find('circle').attributes('data-key'))
    expect(lit.sort()).toEqual(['public.bet', 'public.member'])
    expect(chart.find('g.stars').classes()).toContain('is-lit')
    expect(chart.findAll('line.lit')).toHaveLength(1)
    await chart.find('g.stars').trigger('pointerleave')
    expect(chart.findAll('g.node.lit')).toHaveLength(0)
  })

  it('places each star by a transform, so a new target glides rather than jumps', () => {
    const node = render().find('circle[data-key="public.member"]').element.parentElement
    expect(node?.getAttribute('style')).toContain('translate(0px, 0px)')
  })

  it('rings the target with an HTML pulse, and draws none without a target', () => {
    expect(render().find('span.target-ring').exists()).toBe(true)
    expect(render(null).find('span.target-ring').exists()).toBe(false)
  })
})
