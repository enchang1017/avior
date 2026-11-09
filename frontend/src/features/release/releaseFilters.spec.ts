import { describe, expect, it } from 'vitest'

import { parseQuery } from '@/components'
import { releaseScopes, releaseTabs, visibleReleases } from './releaseFilters'
import type { Release } from './useReleaseQueue'

const base: Release = {
  id: 'rel-1',
  database: 'aviorfixture',
  commitSha: '3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39',
  mergeRequestUrl: 'https://git.example.test/mr/1',
  author: 'chen.yu',
  state: 'pending',
  migrationChecksum: 'sha256:1',
  migrationCount: 1,
  submittedAt: '2026-09-20T08:30:00Z',
}
const releases: Release[] = [
  base,
  { ...base, id: 'rel-2', state: 'approved', author: 'lin.wei', database: 'ledger' },
]
const query = (text: string) => parseQuery(text, releaseScopes)

describe('release filters', () => {
  it('counts each state the list covers, All first', () => {
    expect(releaseTabs(releases, 'pending').map((tab) => [tab.id, tab.count])).toEqual([
      ['', 2],
      ['pending', 1],
      ['approved', 1],
    ])
  })

  it('narrows by state, database and requester, and by words', () => {
    expect(visibleReleases(releases, '', query('state:approved')).map((r) => r.id)).toEqual([
      'rel-2',
    ])
    expect(visibleReleases(releases, '', query('database:ledger')).map((r) => r.id)).toEqual([
      'rel-2',
    ])
    expect(visibleReleases(releases, '', query('requester:chen.yu')).map((r) => r.id)).toEqual([
      'rel-1',
    ])
    expect(visibleReleases(releases, 'pending', query('3f2a9c'))).toHaveLength(1)
    expect(visibleReleases(releases, 'approved', query('requester:chen.yu'))).toEqual([])
  })
})
