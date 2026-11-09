import { describe, expect, it } from 'vitest'

import { gaugeOf } from './gauge'

const limits = { maxCost: 10_000, maxRows: 100_000 }

describe('the plan cost gauge', () => {
  it('reads cost against the cap as a fraction with both values', () => {
    expect(gaugeOf({ estimatedCost: 312, estimatedRows: 3, limits })).toEqual({
      level: 'clear',
      fraction: 0.0312,
      cost: '312',
      maxCost: '10,000',
      rows: '3',
      maxRows: '100,000',
      rowsOver: false,
    })
  })

  it('turns amber at eighty per cent of the cap, the cap itself included', () => {
    expect(gaugeOf({ estimatedCost: 7_999.99, limits }).level).toBe('clear')
    expect(gaugeOf({ estimatedCost: 8_000, limits }).level).toBe('near')
    expect(gaugeOf({ estimatedCost: 10_000, limits }).level).toBe('near')
  })

  it('turns red over the cap and pins the bar full while the number says how far', () => {
    const over = gaugeOf({ estimatedCost: 25_000.5, limits })
    expect(over.level).toBe('over')
    expect(over.fraction).toBe(1)
    expect(over.cost).toBe('25,000.5')
  })

  it('reads no plan, never zero, when nothing was planned', () => {
    const none = gaugeOf({ estimatedCost: null, estimatedRows: null, limits })
    expect(none.level).toBe('no-plan')
    expect(none.fraction).toBeNull()
    expect(none.cost).toBe('no plan')
    expect(none.maxCost).toBe('')
    expect(none.rows).toBe('no plan')
    expect(none.maxRows).toBe('')
    expect(gaugeOf({ limits }).level).toBe('no-plan')
  })

  it('draws no bar where the deployment applies no cap', () => {
    const open = gaugeOf({
      estimatedCost: 5,
      estimatedRows: 1,
      limits: { maxCost: null, maxRows: null },
    })
    expect(open).toMatchObject({
      level: 'clear',
      fraction: null,
      maxCost: 'no cap',
      maxRows: 'no cap',
    })
  })

  it('treats a zero cap as already full', () => {
    expect(gaugeOf({ estimatedCost: 0, limits: { maxCost: 0, maxRows: null } })).toMatchObject({
      level: 'near',
      fraction: 1,
    })
  })

  it('marks estimated rows over the row cap', () => {
    expect(gaugeOf({ estimatedCost: 1, estimatedRows: 200_000, limits }).rowsOver).toBe(true)
  })

  it('says nothing before anything was measured', () => {
    expect(gaugeOf(null)).toMatchObject({ level: 'unmeasured', fraction: null, cost: '—' })
  })
})
