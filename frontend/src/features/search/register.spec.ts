import { createPinia, setActivePinia } from 'pinia'
import { describe, expect, it, vi } from 'vitest'

const push = vi.fn(() => Promise.resolve())
vi.mock('@/router', () => ({ router: { push } }))

describe('search in the palette', () => {
  it('registers pages and the server as two sources, once', async () => {
    setActivePinia(createPinia())
    const { usePalette } = await import('@/shared/palette')
    const before = usePalette().sources.value.length
    await import('./register')
    await import('./register')
    const sources = usePalette().sources.value.slice(before)
    expect(sources).toHaveLength(2)
    const signal = new AbortController().signal
    expect(await sources[0]?.('log', signal)).toEqual([])
    expect(await sources[1]?.('m', signal)).toEqual({ items: [] })
  })
})
