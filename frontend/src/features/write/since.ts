/**
 * How long ago, in the one unit that reads at a glance: `just now`, `12m ago`,
 * `5h ago`, `3d ago`. Hours run to two days, because "30h ago" still says
 * "since yesterday's shift" and "1d ago" hides half a day. A moment in the
 * future, from a clock ahead of this one, reads as now rather than negative.
 *
 * Here rather than beside `stamp`: only the request being read prints an age,
 * and the shared module is in the first load of every page.
 */
export function since(iso: string, now: Date = new Date()): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  const minutes = Math.floor((now.getTime() - at.getTime()) / 60_000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}
