import createClient from 'openapi-fetch'
import type { paths, components } from '@/gen/api'

export type Schemas = components['schemas']
export type GuardVerdict = Schemas['GuardVerdict']
export type GuardReason = Schemas['GuardReason']
export type QueryResult = Schemas['QueryResult']
export type QueryRequest = Schemas['QueryRequest']
export type CurrentUser = Schemas['CurrentUser']
export type TableRef = Schemas['TableRef']
export type ColumnRef = Schemas['ColumnRef']
export type SelectTemplate = Schemas['SelectTemplate']
export type ApiError = Schemas['Error']

const baseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? '/api/v1'

/**
 * How long a request may take before the browser gives up on it.
 *
 * There was no limit, and a request the gateway never answered left its promise
 * pending forever: the spinner turned, the form stayed disabled, and nothing
 * would ever release either. Worse than the one screen — the browser allows six
 * connections per origin over HTTP/1.1, so a handful of requests stuck like that
 * take every one of them and the whole page stops responding, including the
 * attempt to navigate away from it.
 *
 * Above the server's own budget rather than below it. AVIOR_QUERY_TIMEOUT is 30
 * seconds, so a statement that legitimately runs that long must not be cut off
 * here — a client timeout tighter than the server's turns a slow answer into a
 * failure the server never sees.
 */
export const requestTimeoutMs = 45_000

/**
 * timeoutFetch is fetch with a deadline attached.
 *
 * The caller's own signal is kept: openapi-fetch passes one through for requests
 * a view cancels, and replacing it would make a cancelled request run to the
 * deadline anyway. AbortSignal.any is how the two are combined; where it is
 * missing the deadline is dropped rather than the cancellation, because losing
 * the caller's ability to abort is the worse of the two.
 */
function timeoutFetch(input: Request): Promise<Response> {
  if (typeof AbortSignal?.timeout !== 'function') return fetch(input)
  const deadline = AbortSignal.timeout(requestTimeoutMs)
  const signal =
    typeof AbortSignal.any === 'function' ? AbortSignal.any([input.signal, deadline]) : deadline
  return fetch(input, { signal })
}

export const api = createClient<paths>({
  baseUrl,
  credentials: 'include',
  fetch: timeoutFetch,
})

/**
 * Every failure the UI can show. A rejected statement is not an error: it is a
 * verdict the operator needs to read, so it travels in its own shape.
 */
export type Failure =
  | { kind: 'rejected'; verdict: GuardVerdict }
  | { kind: 'unauthorised' }
  | { kind: 'timeout'; message: string }
  | { kind: 'error'; code: string; message: string }

/**
 * describeFailure renders a failure as one line of operator-facing text.
 * Guard rejections list every rule that fired, because fixing only the first
 * one leaves the statement blocked and the operator guessing.
 */
/**
 * unreachable turns a thrown request into a Failure. A rejected fetch is a
 * network or CORS problem, and the operator has to be told rather than left
 * looking at a button that appears to do nothing.
 */
export function unreachable(error: unknown): Failure {
  // A request that hit the deadline above arrives here as an abort, and it is
  // not a network failure: the service was reached and did not answer. Telling
  // an operator it was unreachable would send them to look at the wrong thing.
  if (isAbort(error)) {
    return {
      kind: 'timeout',
      message: `The service did not answer within ${Math.round(requestTimeoutMs / 1000)} seconds.`,
    }
  }
  const detail = error instanceof Error ? error.message : String(error)
  return {
    kind: 'error',
    code: 'network.unreachable',
    message: `Could not reach the Avior service (${detail}).`,
  }
}

/**
 * isAbort recognises the two names a cut-off request comes back under.
 * AbortSignal.timeout raises TimeoutError; an explicitly cancelled one raises
 * AbortError, and both mean the request was stopped rather than refused.
 */
function isAbort(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const name = (error as { name?: unknown }).name
  return name === 'TimeoutError' || name === 'AbortError'
}

export function describeFailure(failure: Failure): string {
  switch (failure.kind) {
    case 'rejected':
      return failure.verdict.reasons.map(describeReason).join(' · ')
    case 'unauthorised':
      return 'Your session has expired. Sign in again.'
    case 'timeout':
      return failure.message
    case 'error':
      return failure.message
  }
}

export function describeReason(reason: GuardReason): string {
  return reason.subject ? `${reason.message} (${reason.subject})` : reason.message
}

export function isApiError(value: unknown): value is ApiError {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate['code'] === 'string' && typeof candidate['message'] === 'string'
}

export function isGuardVerdict(value: unknown): value is GuardVerdict {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Record<string, unknown>
  return (
    (candidate['decision'] === 'allow' || candidate['decision'] === 'deny') &&
    Array.isArray(candidate['reasons'])
  )
}

/**
 * classify turns a response status plus body into a Failure. It is exported so
 * it can be tested without a server; every feature must route errors through
 * it rather than reading response.status directly.
 */
export function classify(status: number, body: unknown): Failure {
  if (status === 401) return { kind: 'unauthorised' }
  if (status === 403 && isGuardVerdict(body)) return { kind: 'rejected', verdict: body }
  if (status === 504) {
    return {
      kind: 'timeout',
      message: isApiError(body) ? body.message : 'The statement exceeded its timeout budget.',
    }
  }
  if (isApiError(body)) return { kind: 'error', code: body.code, message: body.message }
  return { kind: 'error', code: 'unknown', message: `Request failed with status ${status}.` }
}
