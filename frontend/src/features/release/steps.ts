import type { ReleaseDetail } from './useReleaseQueue'
import { stamp } from '@/shared/timestamp'

const when = (iso: string) => stamp(iso)

/** One line under each step of a release: who submitted, what was checked, who decided, how it applied. */
export function releaseStepDetails(detail: ReleaseDetail): string[] {
  const { release, approvals, applications, driftCheck } = detail
  const decided = approvals.at(-1)
  const applied = applications.at(-1)
  const files = release.migrationCount ?? detail.migrations.length
  return [
    `${release.author} · ${when(release.submittedAt)}`,
    `${files} ${files === 1 ? 'file' : 'files'} · drift ${driftCheck?.result ?? 'not checked'}`,
    decided === undefined
      ? release.state === 'pending'
        ? 'Waiting · release.approve'
        : ''
      : `${decided.approver} · ${decided.decision} · ${when(decided.decidedAt)}`,
    applied === undefined
      ? ''
      : `${applied.appliedBy} · ${applied.result === 'failed' ? (applied.error ?? 'failed') : 'applied'} · ${when(applied.startedAt)}`,
  ]
}
