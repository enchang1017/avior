import type { RouteLocationRaw } from 'vue-router'

import type { IconName } from '@/components'
import { isGroup, type NavEntry } from '@/layouts/navigation'
import type { PaletteItem } from '@/shared/palette'

/** A palette result with the heading it is listed under. */
export type SearchItem = PaletteItem & { group: string }

export type PageHit = { label: string; route: string; icon: IconName }

export const maxPageHits = 8

/** Exact, then prefix, then word prefix, then substring; -1 when it does not match at all. */
export function rank(text: string, query: string): number {
  const haystack = text.toLowerCase()
  const needle = query.toLowerCase()
  if (needle === '') return -1
  if (haystack === needle) return 0
  if (haystack.startsWith(needle)) return 1
  if (haystack.split(/[._\s›]+/u).some((word) => word.startsWith(needle))) return 2
  return haystack.includes(needle) ? 3 : -1
}

/** The pages the rail offers, one flat list, a child named with its group. */
export function pagesOf(entries: readonly NavEntry[]): PageHit[] {
  return entries.flatMap((entry): PageHit[] => {
    if (!isGroup(entry)) {
      return entry.route === undefined
        ? []
        : [{ label: entry.label, route: entry.route, icon: entry.icon }]
    }
    return entry.children.flatMap((child) =>
      child.route === undefined
        ? []
        : [{ label: `${entry.label} › ${child.label}`, route: child.route, icon: entry.icon }],
    )
  })
}

export function matchPages(pages: readonly PageHit[], query: string): PageHit[] {
  const text = query.trim()
  return pages
    .map((page) => ({ page, score: rank(page.label, text) }))
    .filter((entry) => entry.score >= 0)
    .sort(
      (a, b) =>
        a.score - b.score ||
        a.page.label.length - b.page.label.length ||
        (a.page.label < b.page.label ? -1 : 1),
    )
    .slice(0, maxPageHits)
    .map((entry) => entry.page)
}

export function pageItems(
  pages: readonly PageHit[],
  query: string,
  go: (to: RouteLocationRaw) => void,
): SearchItem[] {
  return matchPages(pages, query).map((page) => ({
    id: `page:${page.route}`,
    label: page.label,
    hint: 'page',
    icon: page.icon,
    group: 'Pages',
    run: () => go({ name: page.route }),
  }))
}
