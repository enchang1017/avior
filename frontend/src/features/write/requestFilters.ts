import type { ParsedQuery, TabOption } from '@/components'
import type { WriteRequest, WriteRequestState, WriteScope } from './useWriteRequests'
import { scopeStates } from './useWriteRequests'

/** The scopes an update-requests search understands. */
export const requestScopes = ['state', 'database', 'requester'] as const

const labels: Record<WriteRequestState, string> = {
  pending: 'Waiting for review',
  applied: 'Applied',
  rejected: 'Rejected',
  withdrawn: 'Withdrawn',
  failed: 'Failed',
}

/**
 * The status tabs, each counted from the rows the page holds: a count is what
 * was loaded, never an estimate of what exists.
 */
export function stateTabs(requests: readonly WriteRequest[], scope: WriteScope): TabOption[] {
  const count = (state: WriteRequestState) => requests.filter((r) => r.state === state).length
  const states = scopeStates[scope]
  const tabs = states.map((state) => ({ id: state, label: labels[state], count: count(state) }))
  return states.length > 1 ? [{ id: '', label: 'All', count: requests.length }, ...tabs] : tabs
}

function scoped(request: WriteRequest, key: string, value: string): boolean {
  const wanted = value.toLowerCase()
  if (key === 'state') return request.state === wanted
  if (key === 'database') return request.database.toLowerCase() === wanted
  if (key === 'requester') return request.requestedBy.toLowerCase() === wanted
  return true
}

export function matchesQuery(request: WriteRequest, query: ParsedQuery): boolean {
  if (!query.scopes.every((scope) => scoped(request, scope.key, scope.value))) return false
  const haystack = [request.targetTable, request.reason, request.requestedBy, request.database]
    .join(' ')
    .toLowerCase()
  return query.words.every((word) => haystack.includes(word))
}

export function visibleRequests(
  requests: readonly WriteRequest[],
  state: WriteRequestState | '',
  query: ParsedQuery,
): WriteRequest[] {
  return requests.filter(
    (request) => (state === '' || request.state === state) && matchesQuery(request, query),
  )
}
