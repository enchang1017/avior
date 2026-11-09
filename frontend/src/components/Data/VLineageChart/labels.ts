import { sceneHeight, sceneWidth, type ChartStar } from './types'

/** The measures labels are placed with, in scene units. */
export type LabelMetrics = {
  charWidth: number
  lineHeight: number
  /** The target ring's radius, which the focus label sits below. */
  ringRadius: number
  /** Space between a star and its label. */
  gap: number
  /** Space kept clear inside the scene's edges. */
  pad: number
}

export type Box = { left: number; top: number; right: number; bottom: number }

export type LabelBlock = {
  id: string
  x: number
  /** The first line's baseline. */
  y: number
  anchor: 'start' | 'middle' | 'end'
  /** The lines as drawn: the name, then the badge, each truncated to fit. */
  lines: string[]
  /** Which line is the badge, if one is. */
  badgeLine: number | null
  /** The name in full, for the tooltip when it was cut. */
  full: string
  tier: ChartStar['tier']
  box: Box
}

export type LabelInput = Pick<ChartStar, 'id' | 'x' | 'y' | 'tier' | 'labelled'> & {
  text: string
  badge: string | null
  /** The name in full, when the drawn text is a short form of it. */
  title?: string
}

const halfWidth = sceneWidth / 2
const halfHeight = sceneHeight / 2
const ellipsis = '…'
/** The share of a line above its baseline. */
const ascent = 0.8

function truncate(text: string, chars: number): string {
  if (text.length <= chars) return text
  return text.slice(0, Math.max(1, chars - 1)) + ellipsis
}

function outward(star: LabelInput): LabelBlock['anchor'] {
  if (star.tier === 'focus') return 'middle'
  return star.x < -1 ? 'end' : 'start'
}

function labelX(star: LabelInput, anchor: LabelBlock['anchor'], m: LabelMetrics): number {
  if (anchor === 'middle') return star.x
  return anchor === 'start' ? star.x + m.gap : star.x - m.gap
}

/** The side away from the centre, unless the words fit only on the inner side. */
function anchorOf(star: LabelInput, width: number, m: LabelMetrics): LabelBlock['anchor'] {
  const out = outward(star)
  if (out === 'middle' || room(out, labelX(star, out, m), m.pad) >= width) return out
  const inner = out === 'start' ? 'end' : 'start'
  return room(inner, labelX(star, inner, m), m.pad) > room(out, labelX(star, out, m), m.pad)
    ? inner
    : out
}

function room(anchor: LabelBlock['anchor'], x: number, pad: number): number {
  const right = halfWidth - pad - x
  const left = x + halfWidth - pad
  if (anchor === 'start') return right
  if (anchor === 'end') return left
  return 2 * Math.min(left, right)
}

function boxOf(
  x: number,
  y: number,
  width: number,
  height: number,
  anchor: LabelBlock['anchor'],
  m: LabelMetrics,
): Box {
  const left = anchor === 'start' ? x : anchor === 'end' ? x - width : x - width / 2
  const top = y - m.lineHeight * ascent
  return { left, top, right: left + width, bottom: top + height }
}

function block(star: LabelInput, m: LabelMetrics): LabelBlock | null {
  const words = [
    ...(star.labelled ? [star.text] : []),
    ...(star.badge === null ? [] : [star.badge]),
  ]
  if (words.length === 0) return null
  const anchor = anchorOf(star, Math.max(...words.map((word) => word.length)) * m.charWidth, m)
  const x = labelX(star, anchor, m)
  const y =
    anchor === 'middle'
      ? star.y + m.ringRadius + m.lineHeight * ascent
      : star.y + m.lineHeight * (ascent - 0.5)
  const chars = Math.max(4, Math.floor(room(anchor, x, m.pad) / m.charWidth))
  const lines = words.map((word) => truncate(word, chars))
  const width = Math.max(...lines.map((line) => line.length)) * m.charWidth
  return {
    id: star.id,
    x,
    y,
    anchor,
    lines,
    badgeLine: star.badge === null ? null : lines.length - 1,
    full: star.title ?? star.text,
    tier: star.tier,
    box: boxOf(x, y, width, lines.length * m.lineHeight, anchor, m),
  }
}

const crosses = (a: Box, b: Box) =>
  a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom

function moved(label: LabelBlock, dy: number): LabelBlock {
  const { box } = label
  return { ...label, y: label.y + dy, box: { ...box, top: box.top + dy, bottom: box.bottom + dy } }
}

function clamped(label: LabelBlock, pad: number): LabelBlock {
  const top = -halfHeight + pad
  const bottom = halfHeight - pad
  if (label.box.top < top) return moved(label, top - label.box.top)
  if (label.box.bottom > bottom) return moved(label, bottom - label.box.bottom)
  return label
}

/**
 * The labels placed so far, indexed by the bands of rows they cover, so a
 * label is checked against its neighbours rather than every label placed.
 * The first crossing is still the earliest placed, so the answer is the one a
 * scan of every label would give.
 */
class Placed {
  readonly all: LabelBlock[] = []
  private readonly bands = new Map<number, number[]>()

  constructor(private readonly band: number) {}

  private rows(box: Box): number[] {
    const out: number[] = []
    for (
      let row = Math.floor(box.top / this.band);
      row <= Math.floor(box.bottom / this.band);
      row += 1
    )
      out.push(row)
    return out
  }

  add(label: LabelBlock): void {
    const index = this.all.push(label) - 1
    for (const row of this.rows(label.box))
      this.bands.set(row, [...(this.bands.get(row) ?? []), index])
  }

  firstCrossing(box: Box): LabelBlock | undefined {
    let first = Infinity
    for (const row of this.rows(box))
      for (const index of this.bands.get(row) ?? [])
        if (index < first && crosses(box, this.all[index]?.box ?? box)) first = index
    return first === Infinity ? undefined : this.all[first]
  }
}

/** Moves a label past every placed one it crosses, in one direction, until it crosses none. */
function settle(label: LabelBlock, placed: Placed, down: boolean): LabelBlock {
  let current = label
  for (let guard = 0; guard <= placed.all.length; guard += 1) {
    const hit = placed.firstCrossing(current.box)
    if (hit === undefined) return current
    const dy = down ? hit.box.bottom - current.box.top + 1 : hit.box.top - current.box.bottom - 1
    current = moved(current, dy)
  }
  return current
}

function fits(label: LabelBlock, pad: number): boolean {
  return label.box.top >= -halfHeight + pad && label.box.bottom <= halfHeight - pad
}

function freeOf(label: LabelBlock, placed: Placed): boolean {
  return placed.firstCrossing(label.box) === undefined
}

/**
 * Where every label goes: beside its star on the side away from the centre,
 * or on the inner side when only that side has room for it, the focus's below
 * its ring, each cut to fit inside the scene only when neither side can hold
 * it, and nudged
 * downward, or upward when there is no room below, until no two cross. The
 * focus is placed first and everything else in reading order, so the same
 * scene always labels the same way.
 */
export function placeLabels(stars: readonly LabelInput[], m: LabelMetrics): LabelBlock[] {
  const blocks = stars.flatMap((star) => {
    const one = block(star, m)
    return one === null ? [] : [clamped(one, m.pad)]
  })
  blocks.sort(
    (a, b) =>
      Number(b.tier === 'focus') - Number(a.tier === 'focus') ||
      a.box.top - b.box.top ||
      a.box.left - b.box.left ||
      (a.id < b.id ? -1 : 1),
  )
  const placed = new Placed(m.lineHeight * 2)
  for (const label of blocks) {
    const below = settle(label, placed, true)
    if (fits(below, m.pad)) {
      placed.add(below)
      continue
    }
    const above = settle(clamped(below, m.pad), placed, false)
    placed.add(fits(above, m.pad) && freeOf(above, placed) ? above : clamped(below, m.pad))
  }
  return placed.all
}
