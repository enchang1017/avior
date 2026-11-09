import type { TabOption } from '@/components'

export type UsageScope = 'all' | 'own' | null

export type ObservatoryAccess = {
  usage: UsageScope
  /** Capacity, Last used, Performance and Routine timing. */
  instance: boolean
  /** Which FEAT-003 log the caller may open, which decides where a usage row links. */
  auditLog: UsageScope
}

/**
 * What the caller may see. Every signed-in operator sees the whole observatory:
 * the owner opened it to everyone on 2026-09-27 (FEAT-071, Who sees what). The
 * server still narrows each answer to the databases FEAT-058 lets them reach.
 */
export function accessFor(permissions: readonly string[]): ObservatoryAccess {
  const held = new Set(permissions)
  const auditLog = held.has('audit.read.all') ? 'all' : held.has('audit.read.own') ? 'own' : null
  return { usage: 'all', instance: true, auditLog }
}

export type ObservatoryTab =
  | 'overview'
  | 'usage'
  | 'capacity'
  | 'last-used'
  | 'performance'
  | 'routines'

/** FEAT-072: Overview is first for whoever opens any view, drawing only what they may read. */
export function tabsFor(access: ObservatoryAccess): TabOption[] {
  const tabs: TabOption[] = []
  if (access.usage !== null || access.instance) tabs.push({ id: 'overview', label: 'Overview' })
  if (access.usage !== null) tabs.push({ id: 'usage', label: 'Avior usage' })
  if (access.instance) {
    tabs.push(
      { id: 'capacity', label: 'Capacity' },
      { id: 'last-used', label: 'Last used' },
      { id: 'performance', label: 'Performance' },
      { id: 'routines', label: 'Routine timing' },
    )
  }
  return tabs
}

/** The tab asked for when the caller may open it, otherwise their first. */
export function openTab(asked: unknown, tabs: readonly TabOption[]): string {
  const wanted = typeof asked === 'string' ? asked : ''
  return tabs.some((tab) => tab.id === wanted) ? wanted : (tabs[0]?.id ?? '')
}

export type OverviewPanels = {
  /** Usage KPIs, volume, latency and refusal reasons, over whatever usage the caller reads. */
  usage: boolean
  /** Top operators names everybody, so it needs everybody's usage. */
  operators: boolean
  /** Size, idle and long-running KPIs, every server chart and the live panel. */
  server: boolean
}

/** Which parts of the overview are drawn, on FEAT-072 AC-8. */
export function overviewPanels(access: ObservatoryAccess): OverviewPanels {
  return {
    usage: access.usage !== null,
    operators: access.usage === 'all',
    server: access.instance,
  }
}
