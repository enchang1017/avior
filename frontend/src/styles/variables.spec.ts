import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

/*
 * Every custom property a style reads is one somebody declared.
 *
 * A `var()` naming nothing is not an error anywhere: the declaration using it
 * becomes invalid when the value is computed, the property falls back to what
 * it inherits or its initial value, and the page draws something close enough
 * that nobody looks twice. Eight of them shipped that way — a border that never
 * drew, monospace ids set in the body face, a warning colour that was the text
 * colour — under a green `make verify`.
 *
 * A fallback does not excuse the name. `var(--shadow-md, …)` reads as if a
 * shadow token existed, and the page quietly drew whatever the fallback said.
 */

const roots = ['src']

/** Where declarations and uses live: stylesheets, components, and the editor theme. */
const readable = /\.(css|vue|ts)$/u

/**
 * Declared by something other than this codebase, so never in a file here.
 *
 * Tailwind's own `--tw-*` internals and anything it emits are its business; the
 * rule is about the names this code reads, and it reads none of those.
 */
const foreign = /^--tw-/u

function sources(dir: string, found: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) {
      if (entry !== 'gen') sources(path, found)
      continue
    }
    if (readable.test(entry) && !entry.endsWith('.spec.ts')) found.push(path)
  }
  return found
}

/**
 * Names declared as `--name:` anywhere, including inside `@theme` and scoped
 * blocks, and as a key of a bound style object, which is how a component hands a
 * value to its own stylesheet.
 */
function declared(text: string): Set<string> {
  return new Set([...text.matchAll(/(--[\w-]+)['"]?\s*:/gu)].map((found) => found[1] ?? ''))
}

/** Names read with `var(--name` — every one, fallbacks included. */
function referenced(text: string): string[] {
  return [...text.matchAll(/var\(\s*(--[\w-]+)/gu)].map((found) => found[1] ?? '')
}

function undeclared(files: Map<string, string>): string[] {
  const all = new Set<string>()
  for (const text of files.values()) for (const name of declared(text)) all.add(name)

  const missing: string[] = []
  for (const [file, text] of files) {
    for (const name of referenced(text)) {
      if (!all.has(name) && !foreign.test(name)) missing.push(`${file}: ${name}`)
    }
  }
  return [...new Set(missing)]
}

describe('custom properties', () => {
  it('every var() names a property something declares', () => {
    const files = new Map(
      roots.flatMap((root) => sources(root)).map((file) => [file, readFileSync(file, 'utf8')]),
    )
    expect(undeclared(files), 'these read a property nobody declared').toEqual([])
  })

  it('reads the files it claims to', () => {
    const files = roots.flatMap((root) => sources(root))
    expect(files.some((file) => file.endsWith('.css'))).toBe(true)
    expect(files.some((file) => file.includes('editorTheme.ts'))).toBe(true)
    expect(files.filter((file) => file.endsWith('.vue')).length).toBeGreaterThan(20)
  })

  it('tells a declared name from an undeclared one', () => {
    const probe = (css: string) => undeclared(new Map([['probe.css', css]]))

    expect(probe(':root { --a: red } p { color: var(--a) }')).toEqual([])
    expect(probe('p { color: var(--nobody) }')).toEqual(['probe.css: --nobody'])
    // A fallback does not make the name real.
    expect(probe('p { box-shadow: var(--shadow-md, 0 1px red) }')).toEqual([
      'probe.css: --shadow-md',
    ])
    // Declared inside a nested block still counts.
    expect(probe('@theme { --color-x: red } p { color: var(--color-x) }')).toEqual([])
    expect(probe('p { width: var(--tw-anything) }')).toEqual([])
    expect(probe(`:style="{ '--h': x }" p { height: var(--h) }`)).toEqual([])
  })
})
