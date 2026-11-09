import { describe, expect, it } from 'vitest'
import {
  classify,
  describeFailure,
  isApiError,
  isGuardVerdict,
  requestTimeoutMs,
  unreachable,
} from './api'
import type { GuardVerdict } from './api'

const denied: GuardVerdict = {
  decision: 'deny',
  reasons: [
    {
      rule: 'column_denied',
      message: 'caller is not permitted to read this column',
      subject: 'public.member.id_card_number',
    },
    { rule: 'cost_limit_exceeded', message: 'estimated plan cost 58.4 exceeds the limit of 10' },
  ],
  referencedTables: ['public.member'],
  referencedColumns: ['public.member.id_card_number'],
  limits: { maxCost: 50, maxRows: 1000000 },
}

describe('classify', () => {
  it('maps 401 to unauthorised', () => {
    expect(classify(401, null)).toEqual({ kind: 'unauthorised' })
  })

  it('maps a 403 carrying a verdict to a rejection', () => {
    expect(classify(403, denied)).toEqual({ kind: 'rejected', verdict: denied })
  })

  it('does not treat a 403 without a verdict as a rejection', () => {
    const failure = classify(403, { code: 'forbidden', message: 'no' })
    expect(failure.kind).toBe('error')
  })

  it('maps 504 to a timeout with the server message', () => {
    expect(classify(504, { code: 'query.timeout', message: 'over budget' })).toEqual({
      kind: 'timeout',
      message: 'over budget',
    })
  })

  it('falls back to a status-derived message for an unrecognised body', () => {
    expect(classify(500, 'boom')).toEqual({
      kind: 'error',
      code: 'unknown',
      message: 'Request failed with status 500.',
    })
  })
})

describe('describeFailure', () => {
  it('lists every reason so the operator does not fix them one at a time', () => {
    const text = describeFailure({ kind: 'rejected', verdict: denied })
    expect(text).toContain('public.member.id_card_number')
    expect(text).toContain('estimated plan cost')
    expect(text.split(' · ')).toHaveLength(2)
  })

  it('explains an expired session', () => {
    expect(describeFailure({ kind: 'unauthorised' })).toContain('expired')
  })
})

describe('isGuardVerdict', () => {
  it.each([null, undefined, 42, {}, { decision: 'maybe', reasons: [] }, { decision: 'deny' }])(
    'rejects %p',
    (value) => {
      expect(isGuardVerdict(value)).toBe(false)
    },
  )

  it('accepts a well-formed verdict', () => {
    expect(isGuardVerdict(denied)).toBe(true)
  })
})

describe('describeFailure for the remaining kinds', () => {
  it('passes a timeout message through unchanged', () => {
    expect(describeFailure({ kind: 'timeout', message: 'over budget' })).toBe('over budget')
  })

  it('shows the server message for a plain error', () => {
    expect(describeFailure({ kind: 'error', code: 'internal', message: 'boom' })).toBe('boom')
  })
})

describe('isApiError', () => {
  it.each([null, undefined, 'boom', 42, {}, { code: 'x' }, { message: 'y' }])(
    'rejects %p',
    (value) => {
      expect(isApiError(value)).toBe(false)
    },
  )

  it('accepts a well-formed error', () => {
    expect(isApiError({ code: 'internal', message: 'boom' })).toBe(true)
  })
})

describe('unreachable', () => {
  it('reports a request that hit the deadline as a timeout, not as unreachable', () => {
    // What AbortSignal.timeout raises. The service answered nothing; it was not
    // absent, and an operator told otherwise looks at the network instead.
    const timedOut = new DOMException('signal timed out', 'TimeoutError')
    expect(unreachable(timedOut)).toEqual({
      kind: 'timeout',
      message: `The service did not answer within ${Math.round(requestTimeoutMs / 1000)} seconds.`,
    })
  })

  it('reports a cancelled request as a timeout as well', () => {
    expect(unreachable(new DOMException('aborted', 'AbortError')).kind).toBe('timeout')
  })

  it('reports anything else as unreachable, carrying the detail', () => {
    expect(unreachable(new TypeError('Failed to fetch'))).toEqual({
      kind: 'error',
      code: 'network.unreachable',
      message: 'Could not reach the Avior service (Failed to fetch).',
    })
  })
})
