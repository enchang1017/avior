import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { backoff, searchPages, searchServer, type Fetcher } from './engine'
import type { SearchResult } from './hits'

const nav = { go: () => undefined, mayOpen: () => true }

const answer: SearchResult = {
  hits: [{ kind: 'database', label: 'aviorfixture', database: 'aviorfixture' }],
  limit: 20,
  indexing: 2,
}

describe('the server’s half of a search', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('asks nothing for one character or past sixty-four', async () => {
    const fetcher = vi.fn<Fetcher>()
    const signal = new AbortController().signal
    await expect(searchServer('m', signal, nav, fetcher)).resolves.toEqual({
      items: [],
    })
    await expect(searchServer('x'.repeat(65), signal, nav, fetcher)).resolves.toEqual({
      items: [],
    })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('asks once typing has paused for 120 ms, and reports what is still indexing', async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(answer)
    const pending = searchServer(' mem ', new AbortController().signal, nav, fetcher)
    await vi.advanceTimersByTimeAsync(119)
    expect(fetcher).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(fetcher).toHaveBeenCalledWith('mem', expect.any(AbortSignal))
    const result = await pending
    expect(result.items.map((item) => item.label)).toEqual(['aviorfixture'])
    expect(result.status).toBe('still indexing 2 databases')
  })

  it('never asks when the palette moved on during the pause', async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(answer)
    const controller = new AbortController()
    const pending = searchServer('mem', controller.signal, nav, fetcher)
    const settled = expect(pending).rejects.toBeDefined()
    await vi.advanceTimersByTimeAsync(60)
    controller.abort()
    await settled
    await vi.advanceTimersByTimeAsync(200)
    expect(fetcher).not.toHaveBeenCalled()

    const already = new AbortController()
    already.abort()
    await expect(searchServer('mem', already.signal, nav, fetcher)).rejects.toBeDefined()
  })

  it('passes a cancellation on, and turns any other failure into a status line', async () => {
    const controller = new AbortController()
    const cancelled = vi.fn<Fetcher>().mockImplementation(() => {
      controller.abort()
      return Promise.reject(new DOMException('aborted', 'AbortError'))
    })
    const aborted = expect(
      searchServer('mem', controller.signal, nav, cancelled),
    ).rejects.toBeDefined()
    await vi.advanceTimersByTimeAsync(120)
    await aborted

    const broken = vi.fn<Fetcher>().mockRejectedValue(new Error('Search is down (500).'))
    const failing = searchServer('mem', new AbortController().signal, nav, broken)
    await vi.advanceTimersByTimeAsync(120)
    await expect(failing).resolves.toEqual({ items: [], status: 'Search is down (500).' })

    const odd = vi.fn<Fetcher>().mockRejectedValue('nope')
    const strange = searchServer('mem', new AbortController().signal, nav, odd)
    await vi.advanceTimersByTimeAsync(120)
    await expect(strange).resolves.toEqual({ items: [], status: 'Search is unavailable.' })
  })
})

describe('a search asked while the server is still indexing', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  const member: SearchResult = {
    hits: [
      {
        kind: 'table',
        label: 'public.member',
        database: 'aviorfixture',
        schema: 'public',
        name: 'member',
      },
    ],
    limit: 20,
    indexing: 0,
  }
  const indexing = (count: number): SearchResult => ({ hits: [], limit: 20, indexing: count })

  it('waits 1 s, then 2 s, then 4 s at most between asks', () => {
    expect([0, 1, 2, 3, 9].map(backoff)).toEqual([1000, 2000, 4000, 4000, 4000])
  })

  it('asks again until nothing is left to index, and then stops', async () => {
    const fetcher = vi
      .fn<Fetcher>()
      .mockResolvedValueOnce(indexing(2))
      .mockResolvedValueOnce(indexing(1))
      .mockResolvedValueOnce(member)
    const signal = new AbortController().signal
    const first = searchServer('me', signal, nav, fetcher)
    await vi.advanceTimersByTimeAsync(120)
    const early = await first
    expect(early.items).toEqual([])
    expect(early.status).toBe('still indexing 2 databases')

    const second = early.again?.()
    await vi.advanceTimersByTimeAsync(999)
    expect(fetcher).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetcher).toHaveBeenCalledTimes(2)
    const later = await second
    expect(later?.status).toBe('still indexing 1 database')

    const third = later?.again?.()
    await vi.advanceTimersByTimeAsync(1999)
    expect(fetcher).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    const done = await third
    expect(done?.items.map((item) => item.label)).toEqual(['public.member'])
    expect(done?.status).toBe('')
    expect(done?.again).toBeUndefined()
    expect(fetcher).toHaveBeenLastCalledWith('me', signal)
  })

  it('never asks again once the palette has moved on', async () => {
    const fetcher = vi.fn<Fetcher>().mockResolvedValue(indexing(2))
    const controller = new AbortController()
    const first = searchServer('me', controller.signal, nav, fetcher)
    await vi.advanceTimersByTimeAsync(120)
    const again = (await first).again?.()
    const settled = expect(again).rejects.toBeDefined()
    controller.abort()
    await settled
    await vi.advanceTimersByTimeAsync(10_000)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})

describe('the pages half of a search', () => {
  it('lists only what the rail offers this caller, and nothing before sign-in', () => {
    const developer = { roles: [], permissions: ['query.inspect', 'audit.read.own'], features: [] }
    const labels = searchPages('log', developer, () => undefined).map((item) => item.label)
    expect(labels).toEqual(['Personal Log'])
    expect(searchPages('log', null, () => undefined)).toEqual([])
  })
})
