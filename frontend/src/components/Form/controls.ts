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

/** The list a picker opens under itself, floating over the page. */
export const listPanel =
  'list absolute top-full right-0 left-0 z-20 m-0 mt-1 max-h-64 animate-appear list-none overflow-y-auto rounded-card border border-line-strong bg-surface-raised p-0.75 shadow-md'

/** The chevron that opens the list, drawn inside the box's right edge. */
export const caret =
  'caret absolute right-1 flex cursor-pointer items-center border-0 bg-transparent p-1 text-xs text-fg-muted'

type Option = {
  /** A value, the entry that clears the choice, or the note that nothing matched. */
  kind: 'value' | 'clear' | 'none'
  active?: boolean
  chosen?: boolean
  /** Values are identifiers in a face that tells `l` from `1`. */
  mono?: boolean
  /** The note is a sentence, so it may wrap. */
  wrap?: boolean
}

function optionColour(option: Option): string {
  if (option.kind === 'none') return 'text-fg-muted'
  // The clearing entry is not one of the values, so it does not read as one.
  if (option.kind === 'clear') return 'text-fg-muted hover:bg-primary-wash'
  return option.active === true
    ? 'bg-primary-wash text-primary'
    : 'hover:bg-primary-wash hover:text-primary'
}

/** One entry in the list. */
export function optionClass(option: Option): string {
  return [
    'option overflow-hidden rounded-control px-2 py-1.25 text-sm text-ellipsis',
    option.kind === 'none' && option.wrap === true ? 'whitespace-normal' : 'whitespace-nowrap',
    option.kind === 'none' ? 'none cursor-default' : 'cursor-pointer',
    option.kind === 'clear' ? 'clear' : '',
    option.kind === 'value' && option.mono === true ? 'font-mono' : '',
    option.active === true ? 'active' : '',
    option.chosen === true ? 'chosen font-bold' : '',
    optionColour(option),
  ]
    .filter(Boolean)
    .join(' ')
}
