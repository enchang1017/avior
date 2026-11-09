import { afterEach, describe, expect, it, vi } from 'vitest'

import { playScan, scanMs } from './scan'

function stubMotion(reduce: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: reduce && query.includes('reduce') }))
}

describe('the scan line', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('sweeps once, for 600 ms, moving only transform and opacity', () => {
    stubMotion(false)
    const animate = vi.fn(() => ({}) as Animation)
    const element = { animate } as unknown as HTMLElement
    playScan(element)

    const [frames, timing] = animate.mock.calls[0] as unknown as [
      Keyframe[],
      KeyframeAnimationOptions,
    ]
    expect(timing.duration).toBe(scanMs)
    expect(timing.iterations).toBeUndefined()
    for (const frame of frames) {
      expect(Object.keys(frame).sort()).toEqual(['opacity', 'transform'])
    }
  })

  it('does not play for somebody who asked for less motion, or with nothing to play on', () => {
    stubMotion(true)
    const animate = vi.fn()
    expect(playScan({ animate } as unknown as HTMLElement)).toBeNull()
    expect(animate).not.toHaveBeenCalled()
    expect(playScan(null)).toBeNull()
  })
})
