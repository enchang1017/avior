import type { components } from '@/gen/api'
import type { LineageColumnEntry, LineageNode } from './types'

type Schemas = components['schemas']
export type ImpactSummary = Schemas['LineageImpactSummary']
export type ImpactTrigger = Schemas['LineageImpactTrigger']
export type IndirectEntry = Schemas['LineageIndirectEntry']
export type Hop = Schemas['LineageHop']

export type EntryGroup = { title: string; entries: LineageColumnEntry[] }

const isRoutineKind = (node: LineageNode) => node.kind === 'function' || node.kind === 'procedure'
const isViewKind = (node: LineageNode) => node.kind === 'view' || node.kind === 'matview'

function groupOf(entry: LineageColumnEntry): string {
  if (isViewKind(entry.object)) return 'Views'
  if (isRoutineKind(entry.object))
    return entry.access === 'write' ? 'Routines that write' : 'Routines that read'
  return 'Policies, defaults and keys'
}

const groupOrder = [
  'Routines that write',
  'Routines that read',
  'Views',
  'Policies, defaults and keys',
]

/** The direct entries by kind, in a fixed order, each group keeping the answer's order. */
export function groupEntries(entries: readonly LineageColumnEntry[]): EntryGroup[] {
  return groupOrder
    .map((title) => ({ title, entries: entries.filter((entry) => groupOf(entry) === title) }))
    .filter((group) => group.entries.length > 0)
}

function hopText(hop: Hop): string {
  const name = `${hop.node.schema}.${hop.node.name}`
  if (hop.kind === 'trigger') return `trigger ${hop.trigger ?? ''} on ${name}`
  return `${hop.node.kind} ${name}`
}

/** "via view public.member_bets → view public.big_open_bets". */
export function viaText(via: readonly Hop[]): string {
  return `via ${via.map(hopText).join(' → ')}`
}

/** Where an entry's evidence is: its excerpt and its line. */
export function whereText(entry: Pick<LineageColumnEntry, 'excerpt' | 'line'>): string {
  const line = entry.line === undefined ? '' : `line ${entry.line}`
  return [entry.excerpt ?? '', line].filter(Boolean).join(' · ')
}

const nounOf = (entries: readonly LineageColumnEntry[]): [string, string] => {
  if (entries.every((entry) => isRoutineKind(entry.object))) return ['routine', 'routines']
  if (entries.every((entry) => isViewKind(entry.object))) return ['view', 'views']
  return ['object', 'objects']
}

/**
 * A group's count as the summary counts it, by object, saying the statements
 * too when there are more of them: "5 statements in 3 routines".
 */
export function countText(entries: readonly LineageColumnEntry[]): string {
  const objects = new Set(entries.map((entry) => entry.object.id)).size
  const [one, many] = nounOf(entries)
  const noun = objects === 1 ? one : many
  if (entries.length === objects) return `${objects} ${noun}`
  return `${entries.length} statements in ${objects} ${noun}`
}
