import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { writeStepDetails } from './steps'
import type { WriteRequestDetail } from './useWriteRequests'

const request: WriteRequestDetail['request'] = {
  id: 'w1',
  database: 'aviorfixture',
  statement: "UPDATE public.member SET tier = 'gold' WHERE id = 42",
  kind: 'update',
  targetTable: 'public.member',
  reason: 'Support ticket 8812',
  requestedBy: 'chen.yu',
  requestedAt: '2026-09-20T08:30:00Z',
  estimatedRows: 1200,
  state: 'pending',
  reversible: true,
}

describe('the approval steps in words', () => {
  // A step prints the time alone for today, so the day it is read on is fixed.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-09-26T00:00:00Z'))
  })
  afterEach(() => vi.useRealTimers())

  it('says who submitted, what the guard estimated, and who is awaited', () => {
    expect(writeStepDetails({ request, history: [] })).toEqual([
      'chen.yu · 09-20 08:30',
      '1,200 rows estimated · public.member',
      'Waiting · write.approve',
      '',
    ])
  })

  it('names who decided and how the run went', () => {
    const detail: WriteRequestDetail = {
      request: { ...request, state: 'applied', affectedRows: 1 },
      history: [
        { actor: 'chen.yu', action: 'submit', changedAt: '2026-09-20T08:30:00Z' },
        { actor: 'lin.wei', action: 'approve', changedAt: '2026-09-20T09:00:00Z' },
        { action: 'run', changedAt: '2026-09-20T09:00:01Z' },
      ],
    }
    expect(writeStepDetails(detail).slice(2)).toEqual([
      'lin.wei · approve · 09-20 09:00',
      '1 rows · 09-20 09:00',
    ])
  })

  it('carries the error of a failed run, and nothing for a settled request nobody decided', () => {
    const failed: WriteRequestDetail = {
      request: { ...request, state: 'failed', error: 'deadlock detected' },
      history: [{ action: 'fail', changedAt: '2026-09-20T09:00:01Z' }],
    }
    expect(writeStepDetails(failed).slice(2)).toEqual(['', 'deadlock detected · 09-20 09:00'])
    const failedQuietly: WriteRequestDetail = {
      ...failed,
      request: { ...request, state: 'failed' },
    }
    expect(writeStepDetails(failedQuietly)[3]).toBe('failed · 09-20 09:00')
  })
})
