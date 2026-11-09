import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

/*
 * Every pairing of ink and ground the pages draw text with is legible, in the
 * night palette and in the day one.
 *
 * A palette is chosen by looking at it, and looking is exactly the check that
 * misses a muted grey that reads fine on the monitor it was picked on and not
 * on a projector. WCAG AA asks 4.5:1 of body text; the list below is every
 * pair the components actually set text in, and both schemes are measured
 * from the stylesheet itself, so a token edited in one scheme and not the
 * other fails here rather than in somebody's eyes.
 *
 * `fg-faint` is non-text: it is held to 3:1, with a control's edge, the focus
 * ring and the production hairline, rather than to 4.5:1.
 */

const stylesheet = 'src/styles/base.css'

const text = 4.5
/** Non-text UI — a control's edge, a focus ring, a rule a reader needs — at 3:1 (WCAG 1.4.11). */
const ui = 3

const grounds = ['ground', 'surface', 'surface-sunken', 'surface-raised']

const pairs: [ink: string, ground: string][] = [
  ...[
    'fg',
    'fg-soft',
    'fg-muted',
    'primary',
    'target',
    'warn',
    'danger',
    'ok',
    'lineage-read',
    'lineage-write',
  ].flatMap((ink) => grounds.map((ground): [string, string] => [ink, ground])),
  ...['keyword', 'string', 'number', 'bracket-name', 'comment', 'operator'].map(
    (token): [string, string] => [`cm-${token}`, 'surface'],
  ),
  ['on-primary', 'primary'],
  ['on-primary', 'primary-hover'],
  ['on-danger', 'danger'],
  ['primary', 'primary-wash'],
  ['target', 'target-wash'],
  ['warn', 'warn-bg'],
  ['danger', 'danger-bg'],
  ['ok', 'ok-bg'],
  ['env-production', 'env-production-wash'],
  ['env-production', 'surface'],
  ['env-staging', 'env-staging-wash'],
  ['env-staging', 'surface'],
  ['env-local', 'env-local-wash'],
  ['rail-fg', 'rail'],
  ['rail-bright', 'rail-hover'],
  ['fg', 'cm-selection'],
  ['fg', 'primary-soft'],
]

const uiPairs: [mark: string, ground: string][] = [
  ...['fg-faint', 'line-strong', 'primary', 'target', 'env-production', 'danger'].flatMap((mark) =>
    ['ground', 'surface', 'surface-sunken'].map((ground): [string, string] => [mark, ground]),
  ),
  ['line-strong', 'surface-raised'],
  ['chart-edge', 'surface'],
  ['chart-edge-near', 'surface'],
  ...['lineage-read', 'lineage-write', 'star-table', 'star-view', 'star-routine'].flatMap((mark) =>
    grounds.map((ground): [string, string] => [mark, ground]),
  ),
]

type Palette = Map<string, string>

function block(css: string, opener: RegExp): string {
  const found = opener.exec(css)
  if (found === null) throw new Error(`${String(opener)} not found in ${stylesheet}`)
  let depth = 1
  let index = found.index + found[0].length
  const start = index
  while (depth > 0 && index < css.length) {
    if (css[index] === '{') depth += 1
    if (css[index] === '}') depth -= 1
    index += 1
  }
  return css.slice(start, index - 1)
}

function colours(body: string): Palette {
  return new Map(
    [...body.matchAll(/--color-([\w-]+):\s*([^;]+);/gu)].map((m) => [m[1] ?? '', m[2] ?? '']),
  )
}

export function palettes(css: string): { night: Palette; day: Palette } {
  const night = colours(block(css, /@theme static\s*\{/u))
  const day = new Map([...night, ...colours(block(css, /@variant day\s*\{/u))])
  return { night, day }
}

function channels(hex: string): [number, number, number] {
  const match = /^#([0-9a-f]{6})$/iu.exec(hex.trim())
  if (match === null) throw new Error(`${hex} is not a six-digit hex colour`)
  const value = Number.parseInt(match[1] ?? '', 16)
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255]
}

function luminance(hex: string): number {
  const [r, g, b] = channels(hex).map((channel) => {
    const unit = channel / 255
    return unit <= 0.03928 ? unit / 12.92 : ((unit + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (b ?? 0)
}

export function ratio(ink: string, ground: string): number {
  const [light, dark] = [luminance(ink), luminance(ground)].sort((a, b) => b - a)
  return ((light ?? 0) + 0.05) / ((dark ?? 0) + 0.05)
}

export function failures(palette: Palette, scheme: string, list = pairs, minimum = text): string[] {
  const found: string[] = []
  for (const [ink, ground] of list) {
    const fore = palette.get(ink)
    const back = palette.get(ground)
    if (fore === undefined || back === undefined) {
      found.push(`${scheme}: ${ink} on ${ground} — a token is missing`)
      continue
    }
    const measured = ratio(fore, back)
    if (measured < minimum) {
      found.push(`${scheme}: ${ink} on ${ground} is ${measured.toFixed(2)}:1, under ${minimum}:1`)
    }
  }
  return found
}

describe('contrast', () => {
  const { night, day } = palettes(readFileSync(stylesheet, 'utf8'))

  it('every text pair in the night palette meets WCAG AA', () => {
    expect(failures(night, 'night')).toEqual([])
  })

  it('every text pair in the day palette meets WCAG AA', () => {
    expect(failures(day, 'day')).toEqual([])
  })

  it('every non-text mark meets 3:1 in both palettes', () => {
    expect([
      ...failures(night, 'night', uiPairs, ui),
      ...failures(day, 'day', uiPairs, ui),
    ]).toEqual([])
  })

  it('reads both palettes it claims to', () => {
    expect(night.size).toBeGreaterThan(40)
    expect(day.get('ground')).not.toBe(night.get('ground'))
  })

  it('tells a legible pair from an illegible one', () => {
    expect(ratio('#000000', '#ffffff')).toBeCloseTo(21, 0)
    expect(ratio('#767676', '#ffffff')).toBeGreaterThan(4.5)
    expect(ratio('#777777', '#ffffff')).toBeLessThan(4.5)
    const probe = new Map([
      ['ink', '#56607f'],
      ['paper', '#0b1020'],
    ])
    expect(failures(probe, 'probe', [['ink', 'paper']])).toHaveLength(1)
    expect(failures(probe, 'probe', [['absent', 'paper']])).toHaveLength(1)
    // The same pair is a failure as text and a pass as a mark.
    expect(failures(probe, 'probe', [['ink', 'paper']], 3)).toEqual([])
  })
})
