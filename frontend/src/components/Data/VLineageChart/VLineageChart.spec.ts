import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import VLineageChart from './VLineageChart.vue'
import type { ChartEdge, ChartScene, ChartStar } from './types'

const star = (id: string, extra: Partial<ChartStar> = {}): ChartStar => ({
  id,
  shape: 'table',
  label: id,
  title: `public.${id}`,
  x: 0,
  y: 0,
  tier: 'near',
  labelled: true,
  side: 'right',
  hidden: 0,
  degree: 1,
  extension: false,
  badge: null,
  ...extra,
})

const edge = (from: string, to: string, extra: Partial<ChartEdge> = {}): ChartEdge => ({
  key: `${from}>${to}`,
  from,
  to,
  x1: -100,
  y1: 0,
  x2: 100,
  y2: 0,
  tone: 'read',
  parsed: false,
  mark: null,
  far: false,
  step: null,
  highlighted: false,
  caption: `EXACT · ${from} to ${to}`,
  ...extra,
})

const scene: ChartScene = {
  stars: [
    star('bet', { tier: 'focus' }),
    star('settle', { shape: 'function', x: -120, side: 'left' }),
    star('close', { shape: 'procedure', x: 120 }),
    star('seq', { shape: 'sequence', y: 80 }),
    star('daily', { shape: 'matview', y: -80 }),
    star('bets', { shape: 'view', tier: 'far', labelled: false, x: 200, y: 200 }),
    star('ledger', { tier: 'far', hidden: 31, x: -200, y: 200 }),
  ],
  edges: [
    edge('settle', 'bet', { tone: 'write', parsed: true }),
    edge('close', 'bet', { mark: 'foreign_key', tone: 'reference' }),
    edge('bet', 'seq', { mark: 'trigger', tone: 'reference' }),
    edge('bets', 'daily', { far: true }),
  ],
  rings: [130, 226],
  ring: { x: 0, y: 0 },
}

const render = (overrides: Partial<ChartScene> = {}, traceKey = '') =>
  mount(VLineageChart, {
    props: { scene: { ...scene, ...overrides }, summary: 'bet: 6 objects', traceKey },
    attachTo: document.body,
  })

/** The particles' chunk, fetched on intent, has arrived and drawn. */
async function flowArrived(): Promise<void> {
  await vi.dynamicImportSettled()
  await flushPromises()
}

describe('VLineageChart', () => {
  it('draws each kind in its own shape, the focus in amber and the far ones dimmed', () => {
    const chart = render()
    expect(chart.findAll('[data-node]')).toHaveLength(7)
    expect(chart.find('[data-id="settle"]').element.tagName).toBe('path')
    expect(chart.find('[data-id="close"]').element.tagName).toBe('rect')
    expect(chart.find('[data-id="seq"]').element.tagName).toBe('path')
    expect(chart.find('[data-id="daily"]').attributes('stroke-width')).toBe('3')
    expect(chart.find('[data-id="bets"]').classes()).toEqual(
      expect.arrayContaining(['fill-surface', 'far']),
    )
    expect(chart.find('[data-id="bet"]').classes()).toContain('fill-target')
    expect(chart.findAll('ellipse')).toHaveLength(2)
    expect(chart.find('svg').attributes('aria-label')).toBe('bet: 6 objects')
  })

  it('colours by access, dashes what was parsed and marks keys and triggers', () => {
    const chart = render()
    const lines = chart.findAll('[data-edge]')
    expect(lines.map((line) => line.attributes('data-tone'))).toEqual([
      'write',
      'reference',
      'reference',
      'read',
    ])
    expect(lines[0]?.classes()).toContain('stroke-lineage-write')
    expect(lines[0]?.attributes('stroke-dasharray')).toBe('5 4')
    expect(lines[1]?.attributes('stroke-dasharray')).toBe('none')
    expect(lines[1]?.attributes('marker-mid')).toBe('url(#lineage-mark-foreign_key)')
    expect(lines[2]?.attributes('marker-mid')).toBe('url(#lineage-mark-trigger)')
    expect(lines[3]?.classes()).toEqual(expect.arrayContaining(['stroke-lineage-read', 'far']))
    expect(lines[0]?.attributes('d')).toMatch(/^M-100 0 Q.* 100 0$/u)
    expect(lines[0]?.attributes('stroke-width')).toBe('1.75')
    expect(lines[1]?.attributes('stroke-width')).toBe('1.75')
    expect(chart.findAll('.halos path')).toHaveLength(6)
  })

  it('labels a hub with what it hides and leaves unlabelled stars to the hover', async () => {
    const chart = render()
    expect(chart.text()).toContain('ledger +31')
    expect(chart.findAll('text.label').map((label) => label.text())).not.toContain('bets')
    await chart.find('[data-id="bets"]').trigger('pointerover')
    expect(chart.find('[data-testid="lineage-hover-label"]').text()).toBe('public.bets')
  })

  it('chooses a star, and expands a hub instead of choosing it', async () => {
    const chart = render()
    await chart.find('[data-id="settle"]').trigger('click')
    await chart.find('[data-id="ledger"]').trigger('click')
    await chart.find('svg').trigger('click')
    expect(chart.emitted('choose')).toEqual([['settle']])
    expect(chart.emitted('expand')).toEqual([['ledger']])
  })

  it('lights only the hovered star edges, in amber, and puts them out when it leaves', async () => {
    const chart = render()
    await chart.find('[data-id="bet"]').trigger('pointerover')
    const lit = chart.findAll('[data-lit="true"]')
    expect(lit.map((line) => line.attributes('data-edge-key'))).toEqual([
      'settle>bet',
      'close>bet',
      'bet>seq',
    ])
    for (const line of lit) expect(line.classes()).toContain('stroke-target')
    expect(chart.find('[data-edge-key="bets>daily"]').classes()).toContain('stroke-lineage-read')
    await chart.find('[data-id="bet"]').trigger('pointerout')
    expect(chart.findAll('[data-lit="true"]')).toHaveLength(0)
  })

  it('sends six particles along a hovered edge and none once it is left', async () => {
    const chart = render()
    expect(chart.findAll('[data-particle]')).toHaveLength(0)
    await chart.find('svg').trigger('pointerenter')
    await flowArrived()
    const line = chart.find('[data-edge-key="settle>bet"]')
    await line.trigger('pointerover')
    expect(chart.findAll('[data-particle]')).toHaveLength(6)
    expect(chart.find('[data-testid="lineage-hover-label"]').text()).toBe('EXACT · settle to bet')
    await line.trigger('pointerout')
    expect(chart.findAll('[data-particle]')).toHaveLength(0)
    await line.trigger('pointerover')
    await chart.find('svg').trigger('pointerleave')
    expect(chart.findAll('[data-particle]')).toHaveLength(0)
  })

  it('traces the chosen route in order and carries its particles', async () => {
    const chart = render({
      edges: [edge('a', 'b', { step: 1 }), edge('b', 'c', { step: 0 }), edge('c', 'd')],
      stars: [star('a'), star('b'), star('c'), star('d')],
    })
    const traced = chart.findAll('.traced')
    expect(traced.map((line) => line.attributes('data-step'))).toEqual(['1', '0'])
    expect(traced[0]?.attributes('style')).toContain('--step: 1')
    expect(traced[0]?.classes()).toContain('stroke-target')
    await flowArrived()
    expect(chart.findAll('[data-particle]')).toHaveLength(6)
  })

  it('draws the edges that touch the open column in amber', () => {
    const chart = render({ edges: [edge('a', 'b', { highlighted: true }), edge('b', 'c')] })
    const [touching, other] = chart.findAll('[data-edge]')
    expect(touching?.classes()).toContain('stroke-target')
    expect(touching?.attributes('marker-end')).toBe('url(#lineage-arrow-lit)')
    expect(other?.classes()).toContain('stroke-lineage-read')
  })

  it('marks a routine that builds SQL at run time, and its callers, on the star itself', async () => {
    const chart = render({
      stars: [
        star('purge', { shape: 'function', badge: 'dynamic' }),
        star('close', { shape: 'procedure', badge: 'may_reach_unknown', labelled: false }),
      ],
      edges: [edge('close', 'purge', { tone: 'reference' })],
    })
    expect(chart.find('[data-badge="dynamic"]').text()).toBe('DYNAMIC')
    expect(chart.findAll('[data-edge]')).toHaveLength(1)
    await chart.find('[data-id="close"]').trigger('pointerover')
    expect(chart.find('[data-testid="lineage-hover-label"]').text()).toBe(
      'public.close · MAY REACH UNKNOWN',
    )
  })

  it('puts the target ring where the focus is, and none without one', () => {
    const ring = render({ ring: { x: 250, y: -160 } }).find('.target-ring')
    expect(ring.attributes('style')).toContain('left: 75%')
    expect(ring.attributes('style')).toContain('top: 25%')
    expect(render({ ring: null }).find('.target-ring').exists()).toBe(false)
  })

  it('says what every mark means', () => {
    const legend = render().find('[aria-label="Legend"]').text()
    for (const word of ['table', 'view', 'function', 'procedure', 'read', 'write', 'PARSED']) {
      expect(legend).toContain(word)
    }
  })
})
