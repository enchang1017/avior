import type { components } from '@/gen/api'

export type ObservatoryObject = components['schemas']['ObservatoryObject']
export type RoutineGroup = components['schemas']['ObservatoryRoutineGroup']
export type RoutineVersion = components['schemas']['ObservatoryRoutineVersion']

const dayMs = 86_400_000

/**
 * The UTC calendar day of an instant. Snapshots and the flag are both counted
 * in UTC days (FEAT-071), so the date shown is the one the flag was judged on.
 */
export function utcDay(iso: string): string {
  const parsed = new Date(iso)
  return Number.isNaN(parsed.getTime()) ? iso : parsed.toISOString().slice(0, 10)
}

/** Whole UTC calendar days from an instant to now, so the count agrees with the date shown. */
export function daysBetween(iso: string, now: Date): number {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return 0
  const day = (at: Date) => Math.floor(at.getTime() / dayMs)
  return Math.max(0, day(now) - day(parsed))
}

export function daysAgoText(days: number): string {
  if (days <= 0) return 'today'
  return days === 1 ? '1 day ago' : `${days} days ago`
}

/** "last run 2026-09-12 (15 days ago)", the sentence the owner asked for. */
export function lastUsedSentence(
  verb: string,
  lastUsedAt: string,
  daysAgo: number | null,
  now: Date,
): string {
  const days = daysAgo ?? daysBetween(lastUsedAt, now)
  return `last ${verb} ${utcDay(lastUsedAt)} (${daysAgoText(days)})`
}

/**
 * What an object that was never seen used says: since when Avior has been
 * looking, and, once that is longer than the idle window, for how long.
 */
export function neverSeenSentence(trackedSince: string | null, idleDays: number, now: Date) {
  if (trackedSince === null) return 'not seen; tracking has not begun'
  const age = daysBetween(trackedSince, now)
  const since = `not seen since tracking began on ${utcDay(trackedSince)}`
  return age > idleDays ? `${since} (${age} ${age === 1 ? 'day' : 'days'})` : since
}

const kindVerb: Record<ObservatoryObject['kind'], string> = {
  table: 'used',
  index: 'scanned',
  function: 'run',
  procedure: 'run',
}

export function isRoutine(kind: ObservatoryObject['kind']): boolean {
  return kind === 'function' || kind === 'procedure'
}

export type Tracking = {
  idleDays: number
  routinesTrackedSince: string | null
  relationsTrackedSince: string | null
}

export function trackedSinceFor(kind: ObservatoryObject['kind'], tracking: Tracking) {
  return isRoutine(kind) ? tracking.routinesTrackedSince : tracking.relationsTrackedSince
}

/** One object's last use as plain data, never as a verdict. */
export function lastUsedText(object: ObservatoryObject, tracking: Tracking, now: Date): string {
  if (!object.tracked) return `not tracked (${object.notTrackedReason ?? 'no reason given'})`
  if (object.lastUsedAt === null) {
    return neverSeenSentence(trackedSinceFor(object.kind, tracking), tracking.idleDays, now)
  }
  const verb = object.lastUsedKind ?? kindVerb[object.kind]
  return lastUsedSentence(verb, object.lastUsedAt, object.daysAgo, now)
}

const dayWord = (days: number) => (days === 1 ? 'day' : 'days')

export function flagText(idleDays: number): string {
  return `flagged: last use more than ${idleDays} ${dayWord(idleDays)} ago`
}

export function flagPillText(idleDays: number): string {
  return `idle > ${idleDays} ${dayWord(idleDays)}`
}

/** Newest date first, whatever order the versions arrived in. */
export function newestFirst(versions: readonly RoutineVersion[]): RoutineVersion[] {
  return [...versions].sort((a, b) => b.date.localeCompare(a.date) || a.name.localeCompare(b.name))
}

export function groupCountText(group: RoutineGroup): string {
  const count = group.versions.length
  const versions = `${count} ${count === 1 ? 'version' : 'versions'}`
  return group.flaggedCount > 0 ? `${versions} · ${group.flaggedCount} flagged` : versions
}

const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(bytes: number): string {
  let value = Math.abs(bytes)
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  const shown = unit === 0 ? String(value) : value.toFixed(1)
  return `${bytes < 0 ? '−' : ''}${shown} ${units[unit]}`
}

export function formatDelta(now: number | null, then: number | null): string {
  if (now === null || then === null) return '—'
  const delta = now - then
  if (delta === 0) return 'no change'
  return delta > 0 ? `+${formatBytes(delta)}` : formatBytes(delta)
}

export function formatGrowth(bytes: number | null): string {
  if (bytes === null) return '—'
  if (bytes === 0) return 'no change'
  return bytes > 0 ? `+${formatBytes(bytes)}` : formatBytes(bytes)
}

export function formatMs(ms: number | null): string {
  if (ms === null) return '—'
  if (ms < 1000) return `${Math.round(ms * 10) / 10} ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)} s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min ${Math.floor(seconds % 60)} s`
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`
}

export function formatPercent(ratio: number): string {
  return `${(ratio * 100).toFixed(1)}%`
}

/**
 * A count with thousands separators, written out by hand. toLocaleString
 * reaches ICU, whose first call on a page costs tens of milliseconds under the
 * ADR 0009 throttle, and that call landed inside the overview's first render.
 */
export function formatCount(value: number): string {
  const [whole = '0', fraction] = String(Math.round(value * 1000) / 1000).split('.')
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/gu, ',')
  return fraction === undefined ? grouped : `${grouped}.${fraction}`
}
