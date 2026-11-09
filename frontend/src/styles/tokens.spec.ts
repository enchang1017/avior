import { describe, expect, it } from 'vitest'

import {
  boundAttr,
  classesOf,
  each,
  files,
  literals,
  read,
  staticAttr,
  type Source,
} from './sources'

/*
 * Every value a view or a component draws with comes from the theme.
 *
 * `@theme` in base.css is the one place a colour, a size or a measure is
 * decided, and the point of that is that a redesign edits it and nothing else.
 * One `text-[13px]`, one `#475467` in a class map, one `style="width: 18rem"`
 * and that stops being true without anything failing: the page still draws, in
 * a value the theme no longer controls. So the literal itself is the error.
 *
 * What stays allowed: spacing numbers (`p-1.75`, which is the spacing token
 * times a count), fractions, `var(--token)` and `--spacing(n)` inside an
 * arbitrary value, and arbitrary values with no unit at all — a grid template
 * of `auto_1fr`, or a variant like `[&_td]:`.
 */

const roots = ['src/features', 'src/components', 'src/layouts', 'src/shared']

/*
 * The one module that is a stylesheet in object form: CodeMirror's theme. Its
 * colours are tokens like everywhere else, but its lengths are CodeMirror's box
 * model, set once for an editor Tailwind never reaches.
 */
const lengthsExempt = new Set(['src/shared/editorTheme.ts'])

const length =
  /(?<![\w-])-?(\d+\.?\d*|\.\d+)(px|rem|em|ch|ex|vh|vw|vmin|vmax|dvh|svh|lvh|%|pt|cm|mm|in)(?![\w-])/u
const colourFunction = /\b(rgba?|hsla?|hwb|oklch|oklab|lab|lch|color)\(/iu
const colour = new RegExp(`#[0-9a-f]{3,8}(?![\\w-])|${colourFunction.source}`, 'iu')
/** A string that is nothing but one CSS length or colour: a style value in a script. */
const wholeValue =
  /^\s*(-?(\d+\.?\d*|\.\d+)(px|rem|em|ch|ex|vh|vw|%)|#[0-9a-f]{3,8}|(rgba?|hsla?|oklch)\(.*\))\s*$/iu

type Finding = string

/** The bracketed parts of a class: `w-[13px]`, `[&_td]:px-2`, `bg-[#fff]`. */
function arbitrary(candidate: string): string[] {
  return [...candidate.matchAll(/\[([^\]]*)\]/gu)].map((found) => found[1] ?? '')
}

function checkClass(candidate: string, where: string, found: Finding[]): void {
  for (const value of arbitrary(candidate)) {
    if (length.test(value)) found.push(`${where}: ${candidate} — a length the theme does not own`)
    if (colour.test(value)) found.push(`${where}: ${candidate} — a colour the theme does not own`)
  }
}

function checkStyleText(text: string, where: string, found: Finding[], lengths: boolean): void {
  if (colour.test(text)) found.push(`${where}: a colour literal in a style`)
  if (lengths && length.test(text)) found.push(`${where}: a length literal in a style`)
}

function checkTemplate(source: Source, found: Finding[]): void {
  each(source.template, (node) => {
    const where = `${source.file}:${node.loc.start.line}`
    for (const candidate of classesOf(node)) checkClass(candidate, where, found)
    const style = [staticAttr(node, 'style'), boundAttr(node, 'style')].join(' ')
    checkStyleText(style, where, found, true)
    // A prop is a style by another name when its value is one: `max-height="12rem"`.
    for (const prop of node.props ?? []) {
      if (prop.type === 6 && wholeValue.test(prop.value?.content ?? '')) {
        found.push(
          `${where}: ${prop.name}="${prop.value?.content ?? ''}" — a style value as a prop`,
        )
      }
    }
  })
}

function checkScripts(source: Source, found: Finding[], lengths: boolean): void {
  for (const text of source.scripts.flatMap(literals)) {
    const where = `${source.file}: '${text.slice(0, 40)}'`
    for (const candidate of text.split(/\s+/u)) checkClass(candidate, where, found)
    // A script's strings are also messages, where "#8812" and "20%" are words;
    // only a string that is wholly a value, or names a colour function, is a style.
    if (wholeValue.test(text) && (lengths || colour.test(text))) {
      found.push(`${where} — a style value written in a script`)
    } else if (colourFunction.test(text)) {
      found.push(`${where} — a colour literal written in a script`)
    }
  }
}

export function violations(sources: Source[]): Finding[] {
  const found: Finding[] = []
  for (const source of sources) {
    const lengths = !lengthsExempt.has(source.file)
    checkTemplate(source, found)
    checkScripts(source, found, lengths)
    for (const style of source.styles) checkStyleText(style, source.file, found, lengths)
  }
  return found
}

function scanned(): Source[] {
  return roots.flatMap((root) => files(root, /\.(vue|ts)$/u)).map((file) => read(file))
}

describe('tokens only', () => {
  it('no view or component draws with a value the theme does not own', () => {
    expect(violations(scanned()), 'use a theme token instead').toEqual([])
  })

  it('reads the files it claims to', () => {
    const sources = scanned()
    expect(sources.length).toBeGreaterThan(40)
    expect(sources.some((source) => source.file.includes('features/write'))).toBe(true)
    expect(sources.some((source) => source.file.endsWith('variants.ts'))).toBe(true)
  })

  it('tells a token from a literal', () => {
    const vue = (text: string) => violations([read('probe.vue', text)])
    const ts = (text: string) => violations([read('probe.ts', text)])

    expect(vue('<template><p class="text-[13px]" /></template>')).toHaveLength(1)
    expect(vue('<template><p class="max-w-[72ch]" /></template>')).toHaveLength(1)
    expect(vue('<template><p class="w-[84%]" /></template>')).toHaveLength(1)
    expect(vue('<template><p class="bg-[#fff]" /></template>')).toHaveLength(1)
    expect(vue("<template><p :class=\"ok ? 'text-[rgb(0_0_0)]' : ''\" /></template>")).toHaveLength(
      1,
    )
    expect(vue('<template><p style="width: 18rem" /></template>')).toHaveLength(1)
    expect(vue('<template><VSql max-height="12rem" /></template>')).toHaveLength(1)
    expect(vue('<template><p :style="{ width: \'2px\' }" /></template>')).toHaveLength(1)
    expect(vue('<template><p /></template><style>p { color: #101828 }</style>')).toHaveLength(1)
    expect(vue('<template><p /></template><style>p { margin: 0.5rem }</style>')).toHaveLength(1)
    expect(ts("const shadow = '0 1px 2px rgb(0 0 0)'")).toHaveLength(1)
    expect(ts("const height = '18rem'")).toHaveLength(1)
    expect(ts("const classes = 'px-2 [&_td]:py-[3px]'")).toHaveLength(1)

    expect(vue('<template><p class="p-1.75 text-sm max-w-3xl w-1/100" /></template>')).toEqual([])
    expect(vue('<template><p class="grid-cols-[auto_minmax(0,1fr)]" /></template>')).toEqual([])
    expect(
      vue('<template><p class="[:where(&)_td]:px-2.5 [&_.cm-editor]:h-full" /></template>'),
    ).toEqual([])
    expect(vue('<template><p class="grid-cols-[repeat(7,--spacing(8))]" /></template>')).toEqual([])
    expect(vue('<template><p class="max-h-[var(--spacing-tree)]" /></template>')).toEqual([])
    expect(ts("const message = 'Ticket #8812 is 20% done'")).toEqual([])
  })
})
