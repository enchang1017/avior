/**
 * The four things a status can be, and the classes that draw each.
 *
 * Written out in full rather than built from the name: Tailwind finds the
 * utilities it emits by reading the source, and `text-${tone}` is a string it
 * never sees, so a composed class would be one nothing ever generated.
 *
 * `neutral` is a status that is neither good nor bad news — advice, a closed
 * role, an administrator — and it is quieter than the other three on purpose.
 */
export type Tone = 'ok' | 'warn' | 'danger' | 'neutral'

/** The text of a thing in this tone. */
export const toneText: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
  neutral: 'text-fg-soft',
}

/** The ground behind it. */
export const toneWash: Record<Tone, string> = {
  ok: 'bg-ok-bg',
  warn: 'bg-warn-bg',
  danger: 'bg-danger-bg',
  neutral: 'bg-surface-sunken',
}

/** Its hairline border. */
export const toneLine: Record<Tone, string> = {
  ok: 'border-ok-line',
  warn: 'border-warn-line',
  danger: 'border-danger-line',
  neutral: 'border-line-strong',
}

/** The heavier rule down its leading edge, for a card that carries a verdict. */
export const toneAccent: Record<Tone, string> = {
  ok: 'border-l-ok',
  warn: 'border-l-warn',
  danger: 'border-l-danger',
  neutral: 'border-l-fg-soft',
}
