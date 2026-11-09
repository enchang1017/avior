import type { WriteRequestDetail } from './useWriteRequests'
import { stamp } from '@/shared/timestamp'

const when = (iso: string | undefined) => (iso === undefined ? '' : stamp(iso))

const line = (...parts: (string | undefined)[]) =>
  parts.filter((part) => part !== undefined && part !== '').join(' · ')

/** One line under each approval step: who did it, and when, from the request's own history. */
export function writeStepDetails(detail: WriteRequestDetail): string[] {
  const { request, history } = detail
  const last = (action: string) => [...history].reverse().find((change) => change.action === action)
  const decided = last('approve') ?? last('reject') ?? last('withdraw')
  const ran = last('run') ?? last('fail')
  const rows = `${new Intl.NumberFormat('en').format(request.estimatedRows)} rows estimated`
  return [
    line(request.requestedBy, when(request.requestedAt)),
    line(rows, request.targetTable),
    decided === undefined
      ? request.state === 'pending'
        ? 'Waiting · write.approve'
        : ''
      : line(decided.actor, decided.action, when(decided.changedAt)),
    ran === undefined
      ? ''
      : line(
          ran.action === 'fail' ? (request.error ?? 'failed') : `${request.affectedRows ?? 0} rows`,
          when(ran.changedAt),
        ),
  ]
}
