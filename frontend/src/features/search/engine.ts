import type { RouteLocationRaw } from 'vue-router'

import { fetchSearch } from './api'
import { hitItems, indexingNote, type SearchResult } from './hits'
import { navigation, visibleTo, type Caller } from '@/layouts/navigation'
import { pageItems, pagesOf, type SearchItem } from './pages'

export const debounceMs = 120
export const minServerQuery = 2
export const maxServerQuery = 64

export const retryMs = { first: 1000, cap: 4000 }

export type SearchAnswer = {
  items: SearchItem[]
  status?: string
  again?: () => Promise<SearchAnswer>
}

export type Fetcher = (q: string, signal: AbortSignal) => Promise<SearchResult>

/** Where a result goes, and whether the caller may open a page at all. */
export type Navigator = {
  go: (to: RouteLocationRaw) => void
  mayOpen: (route: string) => boolean
}

function pause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason as Error)
      return
    }
    const timer = setTimeout(resolve, ms)
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer)
        reject(signal.reason as Error)
      },
      { once: true },
    )
  })
}

/** 1 s, then 2 s, then every 4 s: how long to wait before asking again. */
export function backoff(attempt: number): number {
  return Math.min(retryMs.first * 2 ** attempt, retryMs.cap)
}

/**
 * The server's half of a search: nothing for one character or past sixty-four,
 * otherwise a request once typing has paused for 120 ms, cancelled with the
 * signal when the palette moves on.
 *
 * While the server is still indexing, the answer is known to be short of what
 * it will be, so it carries a later one: the same query asked again after a
 * backoff, until nothing is left to index or the palette moves on.
 */
export async function searchServer(
  query: string,
  signal: AbortSignal,
  nav: Navigator,
  fetcher: Fetcher = fetchSearch,
  delay = debounceMs,
): Promise<SearchAnswer> {
  const text = query.trim()
  if (text.length < minServerQuery || text.length > maxServerQuery) return { items: [] }
  await pause(delay, signal)
  return askServer(text, signal, nav, fetcher, 0)
}

async function askServer(
  text: string,
  signal: AbortSignal,
  nav: Navigator,
  fetcher: Fetcher,
  attempt: number,
): Promise<SearchAnswer> {
  try {
    const result = await fetcher(text, signal)
    const answer = {
      items: hitItems(result.hits, nav.go, nav.mayOpen),
      status: indexingNote(result),
    }
    if (result.indexing === 0) return answer
    const again = async (): Promise<SearchAnswer> => {
      await pause(backoff(attempt), signal)
      return askServer(text, signal, nav, fetcher, attempt + 1)
    }
    return { ...answer, again }
  } catch (thrown) {
    if (signal.aborted) throw thrown
    return {
      items: [],
      status: thrown instanceof Error ? thrown.message : 'Search is unavailable.',
    }
  }
}

/** The pages the rail offers this caller that match, answered from memory. */
export function searchPages(
  query: string,
  caller: Caller | null,
  go: (to: RouteLocationRaw) => void,
): SearchItem[] {
  return caller === null ? [] : pageItems(pagesOf(visibleTo(navigation, caller)), query, go)
}
