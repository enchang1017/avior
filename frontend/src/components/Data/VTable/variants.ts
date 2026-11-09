/**
 * The shapes a table comes in, and what each draws.
 *
 * Every cell rule sits under `:where()`, which gives it no weight: a utility on
 * the cell itself — `text-right`, `w-1/100` — outranks the table's default for
 * that cell without anybody reaching for `!`.
 *
 * Spelled out in full because Tailwind reads this file to learn which
 * utilities exist, and a class assembled from parts is one it never sees.
 */
export type TableVariant =
  | 'list'
  | 'log'
  | 'requests'
  | 'structure'
  | 'figures'
  | 'record'
  | 'inventory'
  | 'report'
  | 'grid'

/*
 * Every header reads as an instrument's label: small monospace capitals in the
 * muted ink, never competing with the rows under it.
 */
const hudHead =
  '[:where(&)_thead_th]:font-mono [:where(&)_thead_th]:text-2xs [:where(&)_thead_th]:font-medium [:where(&)_thead_th]:tracking-hud [:where(&)_thead_th]:text-fg-muted [:where(&)_thead_th]:uppercase [:where(&)_thead_th]:text-left [:where(&)_thead_th]:whitespace-nowrap'

/** A dense administrative list under a sticky header. */
const listHead = `${hudHead} [:where(&)_thead_th]:sticky [:where(&)_thead_th]:top-0 [:where(&)_thead_th]:z-1 [:where(&)_thead_th]:h-8 [:where(&)_thead_th]:border-b [:where(&)_thead_th]:border-line [:where(&)_thead_th]:bg-surface [:where(&)_thead_th]:px-3`
const listCell =
  '[:where(&)_tbody_td]:ease-colours [:where(&)_tbody_td]:border-b [:where(&)_tbody_td]:border-line [:where(&)_tbody_td]:px-3 [:where(&)_tbody_td]:py-2 [:where(&)_tbody_tr:hover_td]:bg-surface-sunken'

export const tableVariants: Record<TableVariant, string> = {
  list: `w-full border-collapse text-sm ${listHead} ${listCell} [:where(&)_tbody_td]:align-middle`,

  /** The list, with every cell reading from its top: a log row wraps. */
  log: `w-full border-collapse text-sm ${listHead} ${listCell} [:where(&)_tbody_td]:align-top`,

  /**
   * Requests somebody decides on: body-sized, a rule under every row, the row
   * under the pointer held, and a detail row that opens beneath its request.
   */
  requests: `w-full border-collapse text-sm ${hudHead} [:where(&)_thead_th]:h-8 [:where(&)_thead_th]:border-b [:where(&)_thead_th]:border-line [:where(&)_thead_th]:px-3 [:where(&)_td]:border-b [:where(&)_td]:border-line [:where(&)_td]:px-3 [:where(&)_td]:py-2 [:where(&)_td]:text-left [:where(&)_td]:align-top [:where(&)_td]:whitespace-nowrap [:where(&)_tbody_tr:not([data-detail]):hover_td]:bg-surface-sunken [:where(&)_tr[data-detail]_td]:border-t-0 [:where(&)_tr[data-detail]_td]:bg-surface-sunken [:where(&)_tr[data-detail]_td]:px-4 [:where(&)_tr[data-detail]_td]:pt-3 [:where(&)_tr[data-detail]_td]:pb-4 [:where(&)_tr[data-detail]_td]:whitespace-normal`,

  /** A table's own columns: the last row needs no rule, the card edge is one. */
  structure: `w-full border-collapse text-sm ${hudHead} [:where(&)_thead_th]:h-8 [:where(&)_thead_th]:border-b [:where(&)_thead_th]:border-line [:where(&)_thead_th]:px-3 [:where(&)_tbody_td]:border-b [:where(&)_tbody_td]:border-line [:where(&)_tbody_td]:px-2.5 [:where(&)_tbody_td]:py-1.5 [:where(&)_tbody_td]:align-top [:where(&)_tbody_tr:last-child_td]:border-b-0 [:where(&)_tbody_td]:px-3`,

  /** Figures inside a card, flush to its inset: small capitals over numbers. */
  figures:
    'w-full border-collapse text-sm [:where(&)_th]:border-b [:where(&)_th]:border-line [:where(&)_th]:py-1 [:where(&)_th]:pr-2 [:where(&)_th]:pl-0 [:where(&)_th]:text-left [:where(&)_th]:font-mono [:where(&)_th]:text-2xs [:where(&)_th]:font-medium [:where(&)_th]:tracking-hud [:where(&)_th]:text-fg-muted [:where(&)_th]:uppercase [:where(&)_td]:border-b [:where(&)_td]:border-line [:where(&)_td]:py-1 [:where(&)_td]:pr-2 [:where(&)_td]:pl-0 [:where(&)_td]:text-left',

  /**
   * A history under a list: no rules and no header ground, each cell kept to
   * its column so a chip a letter wider does not move the row beside it.
   */
  record:
    'w-auto border-collapse text-xs [:where(&)_th]:pt-1 [:where(&)_th]:pr-3 [:where(&)_th]:pb-1.5 [:where(&)_th]:pl-0 [:where(&)_th]:text-left [:where(&)_th]:align-baseline [:where(&)_th]:font-mono [:where(&)_th]:text-2xs [:where(&)_th]:font-medium [:where(&)_th]:tracking-hud [:where(&)_th]:whitespace-nowrap [:where(&)_th]:text-fg-muted [:where(&)_th]:uppercase [:where(&)_td]:py-1 [:where(&)_td]:pr-3 [:where(&)_td]:pl-0 [:where(&)_td]:text-left [:where(&)_td]:align-baseline [:where(&)_td]:whitespace-nowrap',

  /** Things somebody holds, such as their tokens. */
  inventory:
    'w-full border-collapse text-sm [:where(&)_th]:border-b [:where(&)_th]:border-line [:where(&)_th]:px-2 [:where(&)_th]:py-1.5 [:where(&)_th]:text-left [:where(&)_th]:font-mono [:where(&)_th]:text-2xs [:where(&)_th]:font-medium [:where(&)_th]:tracking-hud [:where(&)_th]:text-fg-muted [:where(&)_th]:uppercase [:where(&)_td]:border-b [:where(&)_td]:border-line [:where(&)_td]:p-2 [:where(&)_td]:align-middle',

  /** A checklist: what is there, what is not, and the one action beside it. */
  report:
    'w-full border-collapse text-md [:where(&)_:is(th,td)]:border-b [:where(&)_:is(th,td)]:border-line [:where(&)_:is(th,td)]:px-3 [:where(&)_:is(th,td)]:py-2 [:where(&)_:is(th,td)]:text-left [:where(&)_:is(th,td)]:align-middle [:where(&)_th]:font-medium [:where(&)_th]:whitespace-nowrap [:where(&)_th]:text-fg [:where(&)_td:last-child]:w-1/100 [:where(&)_td:last-child]:text-right [:where(&)_td:last-child]:whitespace-nowrap',

  /** Query results: monospace, one line per row, the header held in view. */
  grid: `w-full border-separate border-spacing-0 font-mono text-sm whitespace-nowrap [:where(&)_:is(th,td)]:border-b [:where(&)_:is(th,td)]:border-line [:where(&)_:is(th,td)]:px-3 [:where(&)_:is(th,td)]:py-1.5 [:where(&)_:is(th,td)]:text-left [:where(&)_:is(th,td)]:font-normal ${hudHead} [:where(&)_thead_th]:sticky [:where(&)_thead_th]:top-0 [:where(&)_thead_th]:z-1 [:where(&)_thead_th]:h-8 [:where(&)_thead_th]:bg-surface [:where(&)_tbody_tr:hover_td]:bg-surface-sunken [:where(&)_tbody_tr:hover_th]:bg-surface-sunken`,
}
