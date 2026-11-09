import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { releaseStepDetails } from './steps'
import type { ReleaseDetail } from './useReleaseQueue'

const detail: ReleaseDetail = {
  release: {
    id: 'rel-1',
    database: 'aviorfixture',
    commitSha: '3f2a9c1d',
    author: 'chen.yu',
    state: 'pending',
    migrationChecksum: 'sha256:1',
    migrationCount: 2,
    submittedAt: '2026-09-20T08:30:00Z',
  },
  migrations: [],
  relationSizes: [],
  relationsKnown: true,
  readiness: { result: 'match', databaseExists: true },
  lintFindings: [],
  driftCheck: { result: 'match' },
  approvals: [],
  applications: [],
} as unknown as ReleaseDetail

describe('a release’s steps in words', () => {
  // A step prints the time alone for today, so the day it is read on is fixed.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-26T00:00:00Z'))
  })
  afterEach(() => vi.useRealTimers())

  it('names the author, the files and the drift, and who is awaited', () => {
    expect(releaseStepDetails(detail)).toEqual([
      'chen.yu · 09-20 08:30',
      '2 files · drift match',
      'Waiting · release.approve',
      '',
    ])
  })

  it('names the decision and the application', () => {
    const settled = {
      ...detail,
      release: { ...detail.release, state: 'failed', migrationCount: 1 },
      approvals: [{ approver: 'lin.wei', decision: 'approve', decidedAt: '2026-09-20T09:00:00Z' }],
      applications: [
        {
          appliedBy: 'lin.wei',
          startedAt: '2026-09-20T09:05:00Z',
          result: 'failed',
          error: 'lock timeout',
        },
      ],
    } as unknown as ReleaseDetail
    expect(releaseStepDetails(settled)).toEqual([
      'chen.yu · 09-20 08:30',
      '1 file · drift match',
      'lin.wei · approve · 09-20 09:00',
      'lin.wei · lock timeout · 09-20 09:05',
    ])
    const rejected = {
      ...settled,
      release: { ...settled.release, state: 'rejected' },
      approvals: [],
      applications: [{ appliedBy: 'x', startedAt: '2026-09-20T09:05:00Z', result: 'succeeded' }],
    } as unknown as ReleaseDetail
    expect(releaseStepDetails(rejected).slice(2)).toEqual(['', 'x · applied · 09-20 09:05'])
  })
})
