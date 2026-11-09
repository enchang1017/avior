import { beforeAll, describe, expect, it } from 'vitest'

import {
  classesOf,
  each,
  files,
  hasBoundAttr,
  read,
  staticAttr,
  type Source,
  type TemplateNode,
} from '@/styles/sources'
import { utilities, type Utilities } from '@/styles/utilities'

/*
 * The shared look, enforced rather than remembered.
 *
 * A bare `<input>` renders as the browser's own control: a different height, a
 * different border, a different focus ring, sitting beside four things that
 * agreed with each other. It has shipped three times now — a Note field in a
 * dialog, a filter box, a search — and every time `make verify` was green while
 * it did, because nothing here read the templates.
 *
 * So this does. It is a test rather than an ESLint rule because the question is
 * about a whole file's template and the answer is a list of places, which reads
 * better as a failing assertion naming all of them than as one squiggle at a
 * time.
 *
 * "Styled" means Tailwind emits a rule for one of the element's classes, and
 * the compiler that builds the page is the one asked. A class that reads like a
 * utility and draws nothing — a typo, or a name from Tailwind's default theme,
 * which this app clears — is the same mistake one step removed, so it fails
 * here too.
 *
 * What it cannot check: that the utilities chosen look right. That is the
 * parity screenshots' answer. This one answers the question that was actually
 * being got wrong, which is whether a view reached for the shared thing at all.
 */

const viewRoots = ['src/features', 'src/layouts', 'src/shared']
const componentRoot = 'src/components'

/**
 * The elements a browser draws in its own way, which is never this app's.
 *
 * A `<pre>` with no rule is a serif wall; an `<a>` is blue and underlined; a
 * `<button>` is grey and bevelled. Each of these has shipped unstyled at least
 * once, so each is asked the same question: did anybody style it?
 */
const drawnByTheBrowser = new Set(['button', 'a', 'pre'])

/**
 * Elements a view must not write itself, because the library has one.
 *
 * `VSelect` draws its own list rather than the operating system's, `VDialog`
 * is the platform `<dialog>` with the focus trap and the top layer already got
 * right, and `VTable` is every table's rows, rules and header in the shapes
 * the pages share. A view reaching past them gets none of that.
 */
const componentsExistFor = new Map([
  ['select', 'VSelect'],
  ['dialog', 'VDialog'],
  ['table', 'VTable'],
  ['svg', 'VIcon or VLogo'],
  ['i', 'VIcon'],
])

/**
 * PrimeIcons' class family. The font is gone, so a `pi-` class is an icon
 * that draws nothing at all — a blank where a mark was, which no other gate
 * would notice because the element is still there.
 */
const iconFont = /^pi(-|$)/u

/**
 * The types that are text boxes, and the ones that are not.
 *
 * A checkbox styled as a text input is worse than an unstyled one, so the rule
 * covers what VInput is for: things you type into. `search` is included — it
 * is a text box with a clear button.
 */
const notTextBoxes = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'hidden', 'file'])

/**
 * The start of every utility this app writes. A class beginning with one of
 * these is meant as a utility, so it must draw; any other name is a hook for a
 * test or a selector, and carries no style by design. Prefixes a hook would
 * plausibly start with — `order-by`, `object-list`, `row-actions` — are left
 * out, because a gate that fails on a correct name gets switched off.
 */
const utilityShaped =
  /^-?(p[trblxyse]?|m[trblxyse]?|w|h|size|min-w|max-w|min-h|max-h|gap(-[xy])?|space-[xy]|text|bg|border(-[trblxyse])?|rounded(-[trblse]{1,2})?|shadow|font|leading|tracking|z|opacity|inset(-[xy])?|top|right|bottom|left|grid-cols|grid-rows|col-span|row-span|flex|basis|grow|shrink|items|justify|overflow(-[xy])?|whitespace|break|decoration|underline|outline|ring|cursor|pointer-events|animate|transition|duration|ease|translate|rotate|scale|aspect|border-spacing|line-clamp)-/u

type Finding = { file: string; line: number; tag: string; why: string }

/** The class with its variants removed: `hover:[&_td]:px-2` is judged as `px-2`. */
function base(candidate: string): string {
  let depth = 0
  let start = 0
  for (let index = 0; index < candidate.length; index += 1) {
    const char = candidate[index]
    if (char === '[') depth += 1
    else if (char === ']') depth -= 1
    else if (char === ':' && depth === 0) start = index + 1
  }
  return candidate.slice(start).replace(/^!/u, '')
}

function isTextBox(node: TemplateNode): boolean {
  if (node.tag === 'textarea') return true
  return node.tag === 'input' && !notTextBoxes.has(staticAttr(node, 'type') || 'text')
}

function checkElement(node: TemplateNode, file: string, tw: Utilities, found: Finding[]): void {
  const tag = node.tag ?? ''
  const line = node.loc.start.line
  const instead = componentsExistFor.get(tag)
  if (instead !== undefined) {
    found.push({ file, line, tag, why: `use ${instead}` })
  } else if (isTextBox(node)) {
    found.push({ file, line, tag, why: 'use VInput' })
  } else if (drawnByTheBrowser.has(tag)) {
    // A bound class is somebody deciding at runtime, and this cannot follow
    // them there. It is rare enough to be worth reading rather than ruling on.
    const styled = hasBoundAttr(node, 'class') || classesOf(node).some((name) => tw.draws(name))
    if (!styled) found.push({ file, line, tag, why: 'nothing styles it' })
  }
}

/**
 * A view never pins itself to the viewport.
 *
 * Something fixed over the page is an overlay, and ADR 0012 decides where a
 * detail goes: a dialog, a pane that is part of the page, or a page of its
 * own. The side sheet was a fourth answer, and it is gone. The overlays that
 * remain are shared components; a view that wants one reaches for them, and a
 * new kind of overlay is a decision for the ADR rather than for one page.
 */
function checkOverlay(node: TemplateNode, file: string, found: Finding[]): void {
  if (!classesOf(node).some((name) => base(name) === 'fixed')) return
  found.push({
    file,
    line: node.loc.start.line,
    tag: node.tag ?? '',
    why: 'a view lays nothing over the page; see ADR 0012',
  })
}

function checkUtilities(node: TemplateNode, file: string, tw: Utilities, found: Finding[]): void {
  for (const name of classesOf(node)) {
    if (iconFont.test(name)) {
      found.push({
        file,
        line: node.loc.start.line,
        tag: node.tag ?? '',
        why: `${name} is PrimeIcons`,
      })
      continue
    }
    if (name.includes('${')) continue
    if (utilityShaped.test(base(name)) && !tw.draws(name)) {
      const line = node.loc.start.line
      found.push({ file, line, tag: node.tag ?? '', why: `${name} draws nothing` })
    }
  }
}

function scan(sources: Source[], tw: Utilities, views: boolean): Finding[] {
  const found: Finding[] = []
  for (const source of sources) {
    each(source.template, (node) => {
      if (views) checkElement(node, source.file, tw, found)
      if (views) checkOverlay(node, source.file, found)
      checkUtilities(node, source.file, tw, found)
    })
  }
  return found
}

const listed = (findings: Finding[]) =>
  findings.map((found) => `${found.file}:${found.line} <${found.tag}> — ${found.why}`)

const vueUnder = (root: string) => files(root, /\.vue$/u).map((file) => read(file))

describe('the shared look', () => {
  let tw: Utilities
  let views: Source[]
  let components: Source[]
  /*
   * Every template is parsed once here and read by each rule below. Parsing
   * is the cost that remains: measured at 0.25 s on one core, 1.9 s at a third
   * of one (the throttle at which the old per-test parse matched CI's 5.9 s),
   * and 10 s at a tenth, which is the default hook timeout. 30 s covers that
   * tenth three times over.
   */
  beforeAll(async () => {
    tw = await utilities()
    views = viewRoots.flatMap(vueUnder)
    components = vueUnder(componentRoot)
  }, 30_000)

  it('every element a browser draws its own way is styled by somebody', () => {
    expect(
      listed(scan(views, tw, true)),
      'these render the way the browser draws them, beside things that agree with each other',
    ).toEqual([])
  })

  it('every utility a component names draws something', () => {
    expect(listed(scan(components, tw, false))).toEqual([])
  })

  /*
   * Styles live in the one stylesheet and the shared components, nowhere else.
   * A view that carries its own block is a second design system growing inside
   * one page, which is how nine shades of one table happened.
   */
  it('no view carries a stylesheet of its own', () => {
    const styled = views.filter((source) => source.styles.length > 0).map((source) => source.file)
    expect(styled, 'move these styles into utilities or a shared component').toEqual([])
  })

  // The gate is worth nothing if it looks at nothing.
  it('reads the templates it claims to', () => {
    expect(views.length).toBeGreaterThan(25)
    expect(views.some((source) => source.file.includes('features/write'))).toBe(true)
    expect(views.some((source) => source.styles.length === 0)).toBe(true)
    expect(components.length).toBeGreaterThan(15)
  })

  /*
   * And it is worth nothing if it cannot tell the two apart.
   *
   * A rule that passed everything would pass everything silently, which is the
   * failure mode of every gate nobody checked. These are the shapes it exists
   * to separate, written out rather than trusted.
   */
  it('tells a bare control from a styled one', () => {
    const probe = (template: string, asView = true) =>
      scan([read('probe.vue', `<template>${template}</template>`)], tw, asView)

    expect(probe('<input type="text" />')).toHaveLength(1)
    expect(probe('<textarea />')).toHaveLength(1)
    // Nested, because a control inside a dialog is where this got missed.
    expect(probe('<div><section><input /></section></div>')).toHaveLength(1)
    // Nothing styles these at all, which is the wider question.
    expect(probe('<pre>SELECT 1</pre>')).toHaveLength(1)
    expect(probe('<a href="x">y</a>')).toHaveLength(1)
    expect(probe('<pre class="nobody-wrote-this">x</pre>')).toHaveLength(1)
    // And these have a component, so a view must not write its own.
    expect(probe('<select />')).toHaveLength(1)
    expect(probe('<dialog />')).toHaveLength(1)
    expect(probe('<table />')).toHaveLength(1)
    // A detail is a dialog, a pane or a page, never a sheet pinned over one.
    expect(probe('<aside class="fixed inset-y-0 right-0" />')).toHaveLength(1)
    expect(probe("<aside :class=\"open ? 'lg:fixed' : ''\" />")).toHaveLength(1)
    expect(probe('<aside class="fixed inset-y-0 right-0" />', false)).toHaveLength(0)
    // An icon is VIcon, from the registry, and never the retired font.
    expect(probe('<svg viewBox="0 0 1 1" />')).toHaveLength(1)
    expect(probe('<i class="pi pi-search" />')).toHaveLength(3)
    expect(probe('<span class="pi-search" />', false)).toHaveLength(1)
    expect(probe("<span :class=\"['pi', on ? 'pi-eye' : '']\" />")).toHaveLength(2)
    expect(probe('<VIcon name="search" />')).toHaveLength(0)
    // Reads like a utility and draws nothing: a default-theme name, a typo.
    expect(probe('<p class="rounded-lg" />')).toHaveLength(1)
    expect(probe('<p class="text-gray-500" />', false)).toHaveLength(1)
    expect(probe("<p :class=\"on ? 'bg-surfce' : ''\" />")).toHaveLength(1)

    expect(probe('<VInput id="x" />')).toHaveLength(0)
    expect(probe('<pre class="font-mono text-sm">x</pre>')).toHaveLength(0)
    expect(probe('<a class="text-primary underline" href="x">y</a>')).toHaveLength(0)
    expect(probe('<button class="hook cursor-pointer" />')).toHaveLength(0)
    // A checkbox is not what VInput is for.
    expect(probe('<input type="checkbox" />')).toHaveLength(0)
    // A hook names nothing a stylesheet has to draw.
    expect(probe('<p class="node database open [:where(&)_td]:px-2" />')).toHaveLength(0)
    expect(probe('<p class="hover:bg-surface-sunken max-sm:grid-cols-1" />')).toHaveLength(0)
  })
})
