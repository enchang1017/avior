import type { IconName, Tone } from '@/components'
import type { components } from '@/gen/api'

type Release = components['schemas']['Release']
type DriftCheck = NonNullable<components['schemas']['DriftCheck']>
type Readiness = NonNullable<components['schemas']['Readiness']>
type LintFinding = components['schemas']['LintFinding']
type ReleaseState = components['schemas']['ReleaseState']

export type { Tone }

/*
 * The state a release is in is the first thing an approver reads, so applied
 * and failed are told apart by colour rather than by reading the word.
 */
export const stateTone: Record<ReleaseState, 'plain' | 'primary' | Tone> = {
  pending: 'plain',
  approved: 'primary',
  applied: 'ok',
  failed: 'danger',
  rejected: 'danger',
}

export type Verdict = {
  tone: Tone
  icon: IconName
  headline: string
  detail: string
}

export const severityTone: Record<string, Tone> = {
  advice: 'neutral',
  warning: 'warn',
  blocking: 'danger',
}

/**
 * How the drift card reads at a glance.
 *
 * A check that could not run is not a pass, and is graded apart from a drifted
 * target because the two need different things done about them: one is somebody
 * having changed the database, the other is nobody having looked.
 */
export function driftTone(check: DriftCheck | null): Tone {
  if (check === null) return 'neutral'
  if (check.result === 'match') return 'ok'
  return check.result === 'drifted' ? 'danger' : 'warn'
}

export function driftVerdict(check: DriftCheck | null): string {
  if (check === null) return 'Not run yet'
  if (check.result === 'match') return 'No drift'
  return check.result === 'drifted' ? 'Drifted' : 'Not checked'
}

/** The findings gathered by severity, worst first, for the header to count. */
export function countBySeverity(
  findings: LintFinding[],
): { severity: string; tone: Tone; count: number }[] {
  return ['blocking', 'warning', 'advice']
    .map((severity) => ({
      severity,
      tone: severityTone[severity] ?? 'neutral',
      count: findings.filter((finding) => finding.severity === severity).length,
    }))
    .filter((group) => group.count > 0)
}

/**
 * The one line an approver is here for, or null when nobody is asking.
 *
 * Drift and lint were two cards that looked like every other card, so whether a
 * release was safe took reading three paragraphs. This says it once.
 *
 * The order is the order the server refuses in. Readiness first, because it is
 * the live answer and the drift record below it may be from an apply that has
 * since been overtaken; then drift, because an apply against a drifted target
 * is refused whatever the findings say; then blocking findings; then never
 * having looked, which is not a pass either. A settled release returns null:
 * the question is no longer being asked, and a green bar over a release that
 * already failed would be answering a different one.
 */
export function verdictFor(
  release: Release | null,
  drift: DriftCheck | null,
  findings: LintFinding[],
  settled: boolean,
  readiness: Readiness | null = null,
): Verdict | null {
  if (release === null || settled) return null

  // A database nobody has created is the one outcome somebody can act on from
  // inside Avior, and it used to arrive as "the target has drifted" after an
  // apply was pressed. FEAT-059 AC-2.
  if (readiness !== null && readiness.result === 'missing') {
    return {
      tone: 'danger',
      icon: 'warning',
      headline: 'The database does not exist yet',
      detail:
        'Nothing can be applied to it. Create it under Release \u2192 Provisioning, which makes ' +
        'the database, the role that owns what the schema puts in it, and the membership a ' +
        'release needs to change what is already there.',
    }
  }

  if (drift !== null && drift.result !== 'match') {
    return {
      tone: 'danger',
      icon: 'warning',
      headline: 'Do not apply',
      detail:
        drift.result === 'drifted'
          ? 'The target has drifted from what this migration was generated against.'
          : 'The target could not be read, so nothing was checked.',
    }
  }

  const blocking = findings.filter((finding) => finding.severity === 'blocking').length
  if (blocking > 0) {
    return {
      tone: 'danger',
      icon: 'warning',
      headline: 'Do not apply',
      detail: `${blocking} blocking lint finding${blocking === 1 ? '' : 's'}.`,
    }
  }

  if (drift === null) {
    return {
      tone: 'warn',
      icon: 'clock',
      headline: 'Not checked yet',
      detail: 'No drift check has run. One runs before any apply.',
    }
  }

  return {
    tone: 'ok',
    icon: 'circle-check',
    headline: 'Nothing is blocking this',
    detail: 'The target matches, and no finding blocks an apply.',
  }
}
