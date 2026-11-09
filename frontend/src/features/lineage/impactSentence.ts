import type { ImpactSummary } from './impact'

const plural = (count: number, one: string, many: string) => (count === 1 ? one : many)

/**
 * One sentence of what a change reaches: "public.bet.status: 4 routines write
 * it, 3 read it, 2 views depend on it, 1 trigger; 1 DYNAMIC routine may reach
 * it." A part that is zero is left out; "routines" is said once, on the first
 * part that has routines.
 */
export function impactSentence(name: string, summary: ImpactSummary): string {
  const parts: string[] = []
  let routine = true
  const routines = (count: number, verbOne: string, verbMany: string) => {
    const noun = routine ? ` ${plural(count, 'routine', 'routines')}` : ''
    routine = false
    parts.push(`${count}${noun} ${plural(count, verbOne, verbMany)} it`)
  }
  if (summary.writers > 0) routines(summary.writers, 'writes', 'write')
  if (summary.readers > 0) routines(summary.readers, 'reads', 'read')
  if (summary.views > 0)
    parts.push(`${summary.views} ${plural(summary.views, 'view depends', 'views depend')} on it`)
  if (summary.triggers > 0)
    parts.push(`${summary.triggers} ${plural(summary.triggers, 'trigger', 'triggers')}`)
  const dynamic =
    summary.dynamic === 0
      ? ''
      : `${summary.dynamic} DYNAMIC ${plural(summary.dynamic, 'routine', 'routines')} may reach it`
  if (parts.length === 0 && dynamic === '')
    return `${name}: nothing the database holds reads or writes it.`
  const joined = [parts.join(', '), dynamic].filter(Boolean).join('; ')
  return `${name}: ${joined}.`
}
