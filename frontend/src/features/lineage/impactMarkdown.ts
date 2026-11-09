import {
  viaText,
  whereText,
  type EntryGroup,
  type ImpactTrigger,
  type IndirectEntry,
} from './impact'
import { qualified } from './scene'
import type { LineageColumnEntry } from './types'

function entryLine(entry: LineageColumnEntry, via = ''): string {
  const where = whereText(entry)
  const detail = [entry.object.kind, entry.confidence.toUpperCase(), via, where]
    .filter(Boolean)
    .join(' · ')
  return `- \`${qualified(entry.object)}\` — ${detail}`
}

export type ImpactReport = {
  sentence: string
  groups: readonly EntryGroup[]
  triggers: readonly ImpactTrigger[]
  indirect: readonly IndirectEntry[]
  indirectTotal: number
}

/** The panel as Markdown, for a migration MR or a chat. */
export function impactMarkdown(report: ImpactReport): string {
  const sections = [report.sentence]
  for (const group of report.groups)
    sections.push(
      `### ${group.title}\n${group.entries.map((entry) => entryLine(entry)).join('\n')}`,
    )
  if (report.triggers.length > 0) {
    const lines = report.triggers.map(
      (trigger) =>
        `- \`${trigger.name}\` — runs \`${qualified(trigger.function)}\` on ${trigger.events.join(', ')}`,
    )
    sections.push(`### Triggers\n${lines.join('\n')}`)
  }
  if (report.indirect.length > 0) {
    const lines = report.indirect.map((one) => entryLine(one.entry, viaText(one.via)))
    const cut =
      report.indirectTotal > report.indirect.length
        ? `\n\n${report.indirect.length} of ${report.indirectTotal} shown.`
        : ''
    sections.push(`### Indirect\n${lines.join('\n')}${cut}`)
  }
  return `${sections.join('\n\n')}\n`
}
