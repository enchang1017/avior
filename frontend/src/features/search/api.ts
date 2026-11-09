import { api, classify, describeFailure } from '@/shared/api'
import type { SearchResult } from './hits'

/** One search, answered or thrown with a sentence the operator can read. */
export async function fetchSearch(q: string, signal: AbortSignal): Promise<SearchResult> {
  const { data, error, response } = await api.GET('/search', { params: { query: { q } }, signal })
  if (data === undefined) throw new Error(describeFailure(classify(response.status, error)))
  return data
}
