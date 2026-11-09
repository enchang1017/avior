import type { ParsedQuery, TabOption } from '@/components'
import { scopeStates, type Release, type ReleaseScope, type ReleaseState } from './useReleaseQueue'

/** The scopes a release search understands (FEAT-065 AC-9a, AC-11). */
export const releaseScopes = ['state', 'database', 'requester'] as const

const labels: Record<ReleaseState, string> = {
  pending: 'Waiting for review',
  approved: 'Approved',
  applied: 'Applied',
  failed: 'Failed',
  rejected: 'Rejected',
}

/** Status tabs, counted from the page of releases loaded: never an estimate. */
export function releaseTabs(releases: readonly Release[], scope: ReleaseScope): TabOption[] {
  const count = (state: ReleaseState) => releases.filter((r) => r.state === state).length
  return [
    { id: '', label: 'All', count: releases.length },
    ...scopeStates[scope].map((state) => ({
      id: state,
      label: labels[state],
      count: count(state),
    })),
  ]
}

function scoped(release: Release, key: string, value: string): boolean {
  const wanted = value.toLowerCase()
  if (key === 'state') return release.state === wanted
  if (key === 'database') return release.database.toLowerCase() === wanted
  if (key === 'requester') return release.author.toLowerCase() === wanted
  return true
}

export function visibleReleases(
  releases: readonly Release[],
  state: ReleaseState | '',
  query: ParsedQuery,
): Release[] {
  return releases.filter((release) => {
    if (state !== '' && release.state !== state) return false
    if (!query.scopes.every((scope) => scoped(release, scope.key, scope.value))) return false
    const haystack = [release.database, release.author, release.commitSha].join(' ').toLowerCase()
    return query.words.every((word) => haystack.includes(word))
  })
}
