import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { classesOf, each, files, literals, read, type Source } from './sources'

/*
 * Only the compositor moves things.
 *
 * An animation of `transform` or `opacity` runs on the compositor thread: the
 * page is laid out and painted once and the GPU moves the layer. An animation of
 * anything else — a box-shadow pulse, a width, a colour — lays out or paints the
 * page on every frame, on the main thread, where the operator's typing and the
 * result grid also run. The beacon moves all day on production, so a paint per
 * frame there is a cost paid for as long as the tab is open.
 *
 * So every keyframe may touch transform and opacity and nothing else, every
 * transition utility names only those two, and reduced motion switches all of
 * it off in one rule. The trace check in `perf/motion.spec.ts` is the same
 * question asked of the browser; this one is asked of the source, before a
 * browser starts.
 */

const allowed = new Set(['transform', 'opacity', 'translate', 'scale', 'rotate', 'offset'])

const stylesheet = 'src/styles/base.css'
const roots = ['src/features', 'src/components', 'src/layouts', 'src/shared']

const transitionUtility = /^(?:[\w-]+:)*transition(?:-(?<what>[\w[\]-]+))?$/u
const quietTransitions = new Set(['transform', 'opacity', 'none'])

/**
 * Colour may change on an interaction, briefly: ADR 0009 allows 150 ms. It
 * repaints one element and lays nothing out, so it is held to a duration, not
 * banned.
 */
const colours = new Set(['color', 'background-color', 'border-color'])
const colourMs = 150

function milliseconds(token: string | undefined): number {
  const found = /^(\d*\.?\d+)(ms|s)$/u.exec(token ?? '')
  if (found === null) return Infinity
  return Number(found[1]) * (found[2] === 's' ? 1000 : 1)
}

function keyframeBlocks(css: string): { name: string; body: string }[] {
  const found: { name: string; body: string }[] = []
  for (const match of css.matchAll(/@keyframes\s+([\w-]+)\s*\{/gu)) {
    let depth = 1
    let index = (match.index ?? 0) + match[0].length
    const start = index
    while (depth > 0 && index < css.length) {
      if (css[index] === '{') depth += 1
      if (css[index] === '}') depth -= 1
      index += 1
    }
    found.push({ name: match[1] ?? '', body: css.slice(start, index - 1) })
  }
  return found
}

export function keyframeViolations(css: string, where: string): string[] {
  const found: string[] = []
  for (const block of keyframeBlocks(css)) {
    for (const declaration of block.body.matchAll(/([\w-]+)\s*:/gu)) {
      const property = declaration[1] ?? ''
      if (!allowed.has(property)) {
        found.push(`${where}: @keyframes ${block.name} animates ${property}`)
      }
    }
  }
  return found
}

export function transitionDeclarations(css: string, where: string): string[] {
  const found: string[] = []
  for (const match of css.matchAll(/transition(?:-property)?\s*:\s*([^;}]+)/gu)) {
    const parts = (match[1] ?? '')
      .split(',')
      .map((part) => part.trim().split(/\s+/u))
      .filter((part) => (part[0] ?? '') !== '')
    for (const [property = '', duration] of parts) {
      if (quietTransitions.has(property) || property.startsWith('var(')) continue
      if (colours.has(property) && milliseconds(duration) <= colourMs) continue
      found.push(`${where}: a transition of ${property}`)
    }
  }
  return found
}

function transitionClass(candidate: string): string | null {
  const match = transitionUtility.exec(candidate)
  if (match === null) return null
  const what = match.groups?.['what'] ?? 'all'
  return quietTransitions.has(what) ? null : candidate
}

export function classViolations(sources: Source[]): string[] {
  const found: string[] = []
  for (const source of sources) {
    const named: string[] = []
    each(source.template, (node) => named.push(...classesOf(node)))
    for (const text of source.scripts.flatMap(literals)) named.push(...text.split(/\s+/u))
    for (const candidate of named) {
      const loud = transitionClass(candidate)
      if (loud !== null) found.push(`${source.file}: ${loud} transitions more than transform`)
    }
    for (const style of source.styles) {
      found.push(...keyframeViolations(style, source.file))
      found.push(...transitionDeclarations(style, source.file))
    }
  }
  return found
}

/** The spinner keeps turning under reduced motion: it says a request is in flight. */
const informative = new Set(['--animate-spin'])

/**
 * Every infinite animation token that reduced motion leaves running.
 *
 * ADR 0009 answers reduced motion once, by overriding the motion tokens under
 * the media query. So every token whose animation repeats forever must be set
 * to `none` there, except the spinner.
 */
export function stillRunningUnderReducedMotion(css: string): string[] {
  const infinite = [...css.matchAll(/(--animate-[\w-]+):\s*[^;]*\binfinite\b[^;]*;/gu)]
    .map((match) => match[1] ?? '')
    .filter((name) => !informative.has(name))
  const block = /@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{([\s\S]*?)\n\}/u.exec(css)
  const body = block?.[1] ?? ''
  const stilled = new Set(
    [...body.matchAll(/(--animate-[\w-]+):\s*none\s*;/gu)].map((match) => match[1] ?? ''),
  )
  return [...new Set(infinite)].filter((name) => !stilled.has(name))
}

function blocks(css: string, opener: RegExp): { start: number; end: number; body: string }[] {
  const found: { start: number; end: number; body: string }[] = []
  for (const match of css.matchAll(opener)) {
    let depth = 1
    let index = (match.index ?? 0) + match[0].length
    const start = index
    while (depth > 0 && index < css.length) {
      if (css[index] === '{') depth += 1
      if (css[index] === '}') depth -= 1
      index += 1
    }
    found.push({ start: match.index ?? 0, end: index, body: css.slice(start, index - 1) })
  }
  return found
}

type Rule = { selectors: string[]; body: string }

function rules(css: string): Rule[] {
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/gu)].map((match) => ({
    selectors: (match[1] ?? '')
      .split(',')
      .map((selector) => selector.trim())
      .filter(Boolean),
    body: match[2] ?? '',
  }))
}

/** A rule that moves something on its own terms rather than through a motion token. */
function movesLiterally(body: string): boolean {
  const animation = /animation(?:-name)?\s*:\s*([^;]+)/u.exec(body)?.[1]?.trim() ?? ''
  const literalAnimation = animation !== '' && !/^(none|var\(--animate-[\w-]+\))$/u.test(animation)
  const transition = /transition(?:-duration)?\s*:\s*([^;]+)/u.exec(body)?.[1]?.trim() ?? ''
  const literalTransition =
    /(^|[\s,])\d*\.?\d+m?s\b/u.test(transition) && !/^(none|0m?s)$/u.test(transition)
  return literalAnimation || literalTransition
}

function stills(body: string): boolean {
  return /animation\s*:\s*(none|var\(--animate-fade\))|transition(?:-duration)?\s*:\s*(none|0m?s)/u.test(
    body,
  )
}

/**
 * Every animation or timed transition a component's own stylesheet writes has
 * a reduced-motion answer in that stylesheet — unless it goes through a motion
 * token, which base.css answers once for everybody.
 */
export function unansweredMotion(style: string, where: string): string[] {
  const reduced = blocks(style, /@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{/gu)
  let outside = style
  for (const block of [...blocks(style, /@(keyframes|media)[^{]*\{/gu)].reverse()) {
    outside = outside.slice(0, block.start) + outside.slice(block.end)
  }
  const stilled = new Set(
    reduced
      .flatMap((block) => rules(block.body).filter((rule) => stills(rule.body)))
      .flatMap((rule) => rule.selectors),
  )
  return rules(outside)
    .filter((rule) => movesLiterally(rule.body))
    .flatMap((rule) => rule.selectors.filter((selector) => !stilled.has(selector)))
    .map((selector) => `${where}: ${selector} moves under reduced motion`)
}

/** backdrop-filter re-samples everything behind it on every frame (ADR 0009). */
export function backdropFilters(sources: Source[], css: string): string[] {
  const found: string[] = []
  if (/backdrop-filter\s*:/u.test(css)) found.push('base.css: backdrop-filter')
  for (const source of sources) {
    const named: string[] = []
    each(source.template, (node) => named.push(...classesOf(node)))
    for (const text of source.scripts.flatMap(literals)) named.push(...text.split(/\s+/u))
    for (const name of named) {
      if (/(^|:)backdrop-/u.test(name)) found.push(`${source.file}: ${name}`)
    }
    for (const style of source.styles) {
      if (/backdrop-filter\s*:/u.test(style)) found.push(`${source.file}: backdrop-filter`)
    }
  }
  return found
}

/** The longest a path trace can take: FEAT-070 holds it to 600 ms over a five-hop route. */
export function traceLength(css: string, hops = 5): number {
  const trace = /--animate-path-trace:\s*[\w-]+\s+([\d.]+m?s)/u.exec(css)?.[1]
  const stagger = /--stagger-trace:\s*([\d.]+m?s)/u.exec(css)?.[1]
  return milliseconds(trace) + (hops - 1) * milliseconds(stagger)
}

const scanned = () => roots.flatMap((root) => files(root, /\.(vue|ts)$/u)).map((f) => read(f))

describe('motion', () => {
  const css = readFileSync(stylesheet, 'utf8')

  it('every keyframe animates only transform and opacity', () => {
    expect(keyframeViolations(css, stylesheet)).toEqual([])
  })

  it('every transition in the stylesheet names only transform and opacity', () => {
    expect(transitionDeclarations(css, stylesheet)).toEqual([])
  })

  it('no view or component transitions anything the compositor cannot move', () => {
    expect(classViolations(scanned())).toEqual([])
  })

  it('reduced motion stills every infinite animation but the spinner', () => {
    expect(stillRunningUnderReducedMotion(css)).toEqual([])
  })

  it('every component animation has a reduced-motion answer', () => {
    const found = scanned().flatMap((source) =>
      source.styles.flatMap((style) => [
        ...unansweredMotion(style, source.file),
        ...keyframeViolations(style, source.file),
      ]),
    )
    expect(found).toEqual([])
  })

  it('FEAT-074 AC-9: the loader and the sign-in sky move only through tokens that reduced motion stills', () => {
    const feat074 = [
      '--animate-comet',
      '--animate-comet-slow',
      '--animate-drift-a',
      '--animate-drift-b',
      '--animate-pan-far',
      '--animate-pan-mid',
      '--animate-pan-near',
      '--animate-meteor',
    ]
    for (const token of feat074)
      expect(css).toMatch(new RegExp(`${token}:[^;]*\\binfinite\\b`, 'u'))
    expect(stillRunningUnderReducedMotion(css)).toEqual([])
    const reduced = /@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{([\s\S]*?)\n\}/u.exec(
      css,
    )?.[1]
    for (const token of feat074) expect(reduced).toMatch(new RegExp(`${token}:\\s*none;`, 'u'))
    // The route loader's delay survives reduced motion: only its entrance becomes a crossfade.
    expect(reduced).toMatch(/--animate-pending:\s*fade [^;]*\s150ms both;/u)
  })

  it('a path trace plays once and within 600 ms', () => {
    expect(traceLength(css)).toBeLessThanOrEqual(600)
    expect(css).not.toMatch(/--animate-path-trace:[^;]*infinite/u)
  })

  it('nothing uses backdrop-filter', () => {
    expect(backdropFilters(scanned(), css)).toEqual([])
  })

  it('reads the files it claims to', () => {
    expect(keyframeBlocks(css).length).toBeGreaterThan(0)
    expect(scanned().length).toBeGreaterThan(40)
  })

  it('tells a compositor animation from one that paints', () => {
    const probe = (text: string) => keyframeViolations(text, 'probe.css')
    expect(probe('@keyframes a { to { transform: rotate(1turn) } }')).toEqual([])
    expect(probe('@keyframes a { 0%, 100% { opacity: 0 } 50% { opacity: 1 } }')).toEqual([])
    expect(probe('@keyframes a { to { box-shadow: 0 0 0 4px red } }')).toHaveLength(1)
    expect(probe('@keyframes a { from { width: 0 } to { width: 100% } }')).toHaveLength(2)
    expect(probe('@keyframes a { to { background-color: red } }')).toHaveLength(1)

    const moves = (text: string) => transitionDeclarations(text, 'probe.css')
    expect(moves('a { transition: transform 120ms, opacity 120ms }')).toEqual([])
    expect(moves('a { transition: background-color 120ms }')).toEqual([])
    expect(moves('a { transition: background-color 300ms }')).toHaveLength(1)
    expect(moves('a { transition: color 0.2s, border-color 100ms }')).toHaveLength(1)
    expect(moves('a { transition: box-shadow 100ms }')).toHaveLength(1)
    expect(moves('a { transition-property: all }')).toHaveLength(1)

    const vue = (template: string) =>
      classViolations([read('probe.vue', `<template>${template}</template>`)])
    expect(vue('<p class="transition-transform duration-fast" />')).toEqual([])
    expect(vue('<p class="motion-safe:transition-opacity" />')).toEqual([])
    expect(vue('<p class="transition" />')).toHaveLength(1)
    expect(vue('<p class="transition-colors" />')).toHaveLength(1)
    expect(vue('<p class="hover:transition-shadow" />')).toHaveLength(1)
    expect(vue('<p class="transition-[width]" />')).toHaveLength(1)

    const component = `.ring { animation: ring 2s infinite; }
@keyframes ring { to { opacity: 0 } }`
    expect(unansweredMotion(component, 'probe.vue')).toEqual([
      'probe.vue: .ring moves under reduced motion',
    ])
    expect(
      unansweredMotion(
        `${component}\n@media (prefers-reduced-motion: reduce) {\n  .ring { animation: none; }\n}`,
        'probe.vue',
      ),
    ).toEqual([])
    expect(
      unansweredMotion('.node { transition: transform 360ms ease; }', 'probe.vue'),
    ).toHaveLength(1)
    expect(
      unansweredMotion('.node { transition: transform var(--duration-glide) ease; }', 'probe.vue'),
    ).toEqual([])
    expect(unansweredMotion('.chart { animation: var(--animate-chart-in); }', 'probe.vue')).toEqual(
      [],
    )

    expect(traceLength('--animate-path-trace: t 240ms both; --stagger-trace: 72ms;')).toBe(528)
    expect(traceLength('--animate-path-trace: t 1s both; --stagger-trace: 0ms;')).toBe(1000)
    expect(traceLength('')).toBe(Infinity)

    const theme = '--animate-a: a 1s infinite; --animate-spin: spin 1s infinite; --animate-b: b 1s;'
    expect(stillRunningUnderReducedMotion(theme)).toEqual(['--animate-a'])
    expect(
      stillRunningUnderReducedMotion(
        `${theme}\n@media (prefers-reduced-motion: reduce) {\n  :root { --animate-a: none; }\n}`,
      ),
    ).toEqual([])

    expect(
      backdropFilters([read('probe.vue', '<template><p class="backdrop-blur" /></template>')], ''),
    ).toHaveLength(1)
    expect(backdropFilters([], 'p { backdrop-filter: blur(4px) }')).toHaveLength(1)
    expect(
      backdropFilters([read('probe.vue', '<template><p class="blur-none" /></template>')], ''),
    ).toEqual([])
  })
})
