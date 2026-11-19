import { lazy } from '@/shared/lazy'

/**
 * The look every control that is a box shares: the pickers, the calendar's
 * trigger and its clock fields all read as the text box beside them — the same
 * height, border, ground and focus ring.
 *
 * Width and face are left to the caller, because a clock field is narrow and
 * monospace and a picker fills its cell; setting either here would put two
 * utilities for one property on the same element.
 */
export const controlBox =
  'ease-colours h-control rounded-control border border-line-strong bg-surface-sunken text-fg placeholder:text-fg-muted hover:border-fg-muted focus:border-primary focus:outline-none'

/** The inset and type of a box, which a small one draws a step tighter. */
export function controlSize(size: 'md' | 'sm'): string {
  return size === 'sm' ? 'py-0.75 text-sm' : 'py-1 text-md'
}

/** Options are loaded only when a picker is opened. */
export const OptionList = lazy(
  () => import('./VSelect/OptionList.vue'),
  'Loading options',
  'inline',
)

/** The chevron that opens the list, drawn inside the box's right edge. */
export const caret =
  'caret absolute right-1 flex cursor-pointer items-center border-0 bg-transparent p-1 text-xs text-fg-muted'
