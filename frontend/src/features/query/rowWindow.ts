/**
 * Which rows of a long result are in the DOM. ADR 0009 P17: never more than
 * sixty, however many came back, so a 100,000-row result costs what a screen
 * of it costs.
 */
export const maxRendered = 60
const overscan = 8

export type RowWindow = { start: number; end: number; before: number; after: number }

export function rowWindow(
  total: number,
  scrollTop: number,
  viewport: number,
  rowHeight: number,
): RowWindow {
  const height = Math.max(1, rowHeight)
  const visible = Math.ceil(Math.max(0, viewport) / height)
  const count = Math.min(total, maxRendered, visible + overscan * 2)
  const first = Math.floor(Math.max(0, scrollTop) / height) - overscan
  const start = Math.max(0, Math.min(first, total - count))
  const end = start + count
  return { start, end, before: start * height, after: (total - end) * height }
}
