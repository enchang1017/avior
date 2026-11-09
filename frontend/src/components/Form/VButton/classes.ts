export type ButtonVariant = 'primary' | 'ghost' | 'bare'
export type ButtonSize = 'md' | 'sm'

/*
 * `font-[inherit]` rather than the browser's control font: a button carries its
 * own face, and without it every button sat in a different typeface from the
 * text it acts on. The size, weight and leading are set explicitly beside it.
 *
 * The height is fixed, not a floor: these sit in flex rows that stretch their
 * children, so a floor leaves a button as tall as whatever is beside it rather
 * than the one height every control claims. The leading is fixed for the same
 * reason, because an icon's line box is taller than the text's.
 *
 * `no-underline`: some of these are links, because going somewhere is
 * navigation rather than an action, and a link that looks like a button has to
 * look like one all the way.
 */
const base =
  'press ease-colours inline-flex cursor-pointer items-center justify-center rounded-control border font-[inherit] leading-snug font-medium whitespace-nowrap no-underline'

/*
 * Controls run a step below body text. The page is read at 16px; the chrome
 * around it does not need to be, and at 16px the toolbar buttons were 41px tall
 * and dominated the thing they act on.
 */
const sizes: Record<ButtonSize, string> = {
  md: 'h-control gap-1.5 text-sm',
  sm: 'h-control-sm gap-1 text-xs',
}

/** A bare button is inset the same at either size: it has no edge to pad from. */
function padding(variant: ButtonVariant, size: ButtonSize): string {
  if (variant === 'bare') return 'px-2 py-1'
  return size === 'sm' ? 'px-2 py-0.5' : 'px-3 py-1'
}

/*
 * The primary's disabled state is a pale primary, not grey: it reads as "not
 * yet" rather than "broken".
 *
 * `not-disabled:hover` rather than `enabled:hover`, because a link is never
 * `:enabled` and a link-button has to answer the pointer as well.
 *
 * The text colour is kept apart from the rest, because a destructive action
 * replaces it: red whether the button is enabled or not, so an action that
 * destroys something reads as one before anybody can take it.
 */
type Look = { frame: string; text: string }

const variants: Record<ButtonVariant, Look> = {
  primary: {
    frame:
      'border-primary bg-primary shadow-glow-primary not-disabled:hover:border-primary-hover not-disabled:hover:bg-primary-hover disabled:cursor-not-allowed disabled:border-primary-soft disabled:bg-primary-soft disabled:shadow-none',
    text: 'text-on-primary disabled:text-fg-muted',
  },
  ghost: {
    frame:
      'border-line-strong bg-transparent not-disabled:hover:bg-surface-sunken disabled:cursor-not-allowed',
    text: 'text-fg-soft not-disabled:hover:text-fg disabled:text-fg-muted',
  },
  bare: {
    frame: 'border-transparent bg-transparent not-disabled:hover:bg-surface-sunken',
    text: 'text-fg-soft not-disabled:hover:text-fg',
  },
}

function text(variant: ButtonVariant, danger: boolean): string {
  if (!danger) return variants[variant].text
  // A bare button still brightens under the pointer, as it did before.
  return variant === 'bare'
    ? 'danger-text text-danger not-disabled:hover:text-fg'
    : 'danger-text text-danger'
}

/** Every class a button carries. */
export function buttonClasses(variant: ButtonVariant, size: ButtonSize, danger: boolean): string {
  return [
    base,
    sizes[size],
    padding(variant, size),
    variants[variant].frame,
    text(variant, danger),
  ].join(' ')
}
