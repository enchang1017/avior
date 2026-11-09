import { describe, expect, it } from 'vitest'

import { countBySeverity, driftTone, driftVerdict, verdictFor } from './verdict'
import type { components } from '@/gen/api'

type DriftCheck = NonNullable<components['schemas']['DriftCheck']>
type LintFinding = components['schemas']['LintFinding']
type Release = components['schemas']['Release']

const release = { state: 'pending' } as Release

function drift(result: DriftCheck['result']): DriftCheck {
  return { result, checkedAt: '2026-09-05T01:21:21Z', detail: '' }
}

function finding(severity: string): LintFinding {
  return { severity, rule: 'a-rule', message: 'something' } as LintFinding
}

describe('the drift card', () => {
  it('reads as clear only when the target matches', () => {
    expect(driftTone(drift('match'))).toBe('ok')
    expect(driftVerdict(drift('match'))).toBe('No drift')
  })

  // Unavailable is graded apart from drifted rather than lumped in with it:
  // one is somebody having changed the database, the other is nobody having
  // looked, and they need different things done about them.
  it('tells a drifted target apart from one nobody could read', () => {
    expect(driftTone(drift('drifted'))).toBe('danger')
    expect(driftTone(drift('unavailable'))).toBe('warn')
    expect(driftVerdict(drift('unavailable'))).toBe('Not checked')
  })

  it('says so when no check has run at all', () => {
    expect(driftTone(null)).toBe('neutral')
    expect(driftVerdict(null)).toBe('Not run yet')
  })
})

describe('the finding counts', () => {
  it('puts the worst severity first, whatever order they arrived in', () => {
    const counted = countBySeverity([finding('advice'), finding('blocking'), finding('warning')])

    expect(counted.map((group) => group.severity)).toEqual(['blocking', 'warning', 'advice'])
  })

  it('names only the severities that are present', () => {
    expect(countBySeverity([finding('advice'), finding('advice')])).toEqual([
      { severity: 'advice', tone: 'neutral', count: 2 },
    ])
  })

  // Advice is not a problem. Colouring it as one makes the colour stop meaning
  // anything on the release that does have a problem.
  it('leaves advice reading as neutral', () => {
    expect(countBySeverity([finding('advice')])[0]?.tone).toBe('neutral')
    expect(countBySeverity([finding('warning')])[0]?.tone).toBe('warn')
    expect(countBySeverity([finding('blocking')])[0]?.tone).toBe('danger')
  })
})

describe('verdictFor', () => {
  it('clears a release whose target matches and whose findings are advice', () => {
    const verdict = verdictFor(release, drift('match'), [finding('advice')], false)

    expect(verdict?.tone).toBe('ok')
  })

  /*
   * The order is the order the server refuses in.
   *
   * An apply against a drifted target is refused whatever the findings say, so
   * drift is named first: telling an approver about a lint finding when the
   * real answer is "the database moved" sends them to fix the wrong thing.
   */
  it('names drift before a blocking finding when both are true', () => {
    const verdict = verdictFor(release, drift('drifted'), [finding('blocking')], false)

    expect(verdict?.tone).toBe('danger')
    expect(verdict?.detail).toContain('drifted')
  })

  it('refuses on a blocking finding when the target matches', () => {
    const verdict = verdictFor(release, drift('match'), [finding('blocking')], false)

    expect(verdict?.headline).toBe('Do not apply')
    expect(verdict?.detail).toBe('1 blocking lint finding.')
  })

  it('counts more than one blocking finding, in words that read', () => {
    const verdict = verdictFor(
      release,
      drift('match'),
      [finding('blocking'), finding('blocking')],
      false,
    )

    expect(verdict?.detail).toBe('2 blocking lint findings.')
  })

  // Never having looked is not a pass. It is not a refusal either, because a
  // check runs before any apply — so it is the one case that reads as amber.
  it('holds a release nobody has checked, without calling it a refusal', () => {
    const verdict = verdictFor(release, null, [], false)

    expect(verdict?.tone).toBe('warn')
    expect(verdict?.headline).toBe('Not checked yet')
  })

  it('refuses a target that could not be read', () => {
    expect(verdictFor(release, drift('unavailable'), [], false)?.tone).toBe('danger')
  })

  // A settled release is not a question. A green bar over one that already
  // failed would be answering a different one.
  it('says nothing about a release that is already settled', () => {
    expect(verdictFor(release, drift('match'), [], true)).toBeNull()
  })

  it('says nothing before the release has loaded', () => {
    expect(verdictFor(null, null, [], false)).toBeNull()
  })
})

/*
 * FEAT-059 AC-2. A database nobody has created is not a database that drifted,
 * and the difference is the whole point: one is somebody having changed
 * production by hand, the other is nobody having made it yet. The second used
 * to arrive as the first, after an apply had already been pressed.
 */
describe('verdictFor with readiness', () => {
  const pending = { state: 'pending' } as Parameters<typeof verdictFor>[0]

  it('names the missing database rather than reporting drift', () => {
    const verdict = verdictFor(pending, null, [], false, {
      result: 'missing',
      databaseExists: false,
    })

    expect(verdict?.headline).toBe('The database does not exist yet')
    expect(verdict?.detail).toContain('Provisioning')
  })

  // It is read before the drift record because that record may be from an apply
  // the world has moved past, and this one was computed when the page opened.
  it('is preferred over a drift record that says something else', () => {
    const verdict = verdictFor(
      pending,
      { result: 'drifted', checkedAt: '2026-09-19T00:00:00Z' },
      [],
      false,
      { result: 'missing', databaseExists: false },
    )

    expect(verdict?.headline).toBe('The database does not exist yet')
  })

  it('leaves the drift verdict alone once the database is there', () => {
    const verdict = verdictFor(
      pending,
      { result: 'drifted', checkedAt: '2026-09-19T00:00:00Z' },
      [],
      false,
      { result: 'drifted', databaseExists: true },
    )

    expect(verdict?.headline).toBe('Do not apply')
  })
})
