/**
 * How every page prints a moment: in UTC, the time the audit log and every
 * server record are kept in and the time the HUD's clock shows.
 *
 * The request sheet's style, `MM-DD HH:MM`, shortened to `HH:MM` for today and
 * lengthened to `YYYY-MM-DD HH:MM` outside this year. Pages printed dates in
 * the browser's locale and zone beside a sheet and a sky axis in UTC, so one
 * screen could name one moment twice, hours apart, in two languages. The full
 * ISO timestamp is the title, for whoever needs the seconds.
 *
 * Not in `clock.ts`: that module is in the shell, and only pages print records.
 */
function parsed(iso: string): Date | null {
  const at = new Date(iso)
  return Number.isNaN(at.getTime()) ? null : at
}

export function stamp(iso: string, now: Date = new Date()): string {
  const at = parsed(iso)
  if (at === null) return iso
  const full = at.toISOString()
  const time = full.slice(11, 16)
  if (full.slice(0, 10) === now.toISOString().slice(0, 10)) return time
  if (full.slice(0, 4) === now.toISOString().slice(0, 4)) return `${full.slice(5, 10)} ${time}`
  return `${full.slice(0, 10)} ${time}`
}

export function fullStamp(iso: string): string {
  return parsed(iso)?.toISOString() ?? iso
}
