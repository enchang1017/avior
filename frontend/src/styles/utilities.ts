import { readFile } from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, resolve } from 'node:path'

import { __unstable__loadDesignSystem as loadDesignSystem } from 'tailwindcss'

/*
 * Tailwind's own answer to "does this class draw anything", for the style gates.
 *
 * The gates ask it rather than keep a list, because a list is a second theme to
 * fall out of step with the first: `rounded-lg` reads like a utility and, with
 * Tailwind's default theme cleared, produces nothing at all. Only the compiler
 * that builds the page knows which names the page's stylesheet makes real.
 */

// Relative to the frontend, where every gate runs from, like the specs' own paths.
const stylesheet = resolve('src/styles/base.css')
const modules = resolve('node_modules')
const tailwindRoot = join(
  dirname(createRequire(resolve('package.json')).resolve('tailwindcss')),
  '..',
)

function locate(id: string, base: string): string {
  if (id.startsWith('.')) return resolve(base, id)
  if (id.startsWith('tailwindcss/')) return join(tailwindRoot, id.slice('tailwindcss/'.length))
  return join(modules, id)
}

async function load(id: string, base: string) {
  const path = locate(id, base)
  return { path, base: dirname(path), content: await readFile(path, 'utf8') }
}

export type Utilities = {
  /** Whether Tailwind emits a rule for this exact class, variants included. */
  draws(candidate: string): boolean
}

/**
 * Loads the app's stylesheet once; each question after that compiles one class.
 *
 * Asked of the design system rather than `compile()`, because `build()` is the
 * page's build: each class it has not seen re-emits every rule gathered so far,
 * so asking one class at a time costs the square of the classes asked — over
 * half a second locally, past the test timeout on a loaded runner.
 * `candidatesToCss` compiles only the classes it is handed, from the same
 * stylesheet, and returns null for one that draws nothing. The `__unstable__`
 * export is held still by the exact tailwindcss version in package.json, and
 * the probes in `styleContract.spec.ts` ask it for classes that draw and
 * classes that do not, so an upgrade that moves it fails there.
 */
export async function utilities(): Promise<Utilities> {
  const design = await loadDesignSystem(readFileSync(stylesheet, 'utf8'), {
    base: dirname(stylesheet),
    loadStylesheet: load,
  })
  const known = new Map<string, boolean>()

  return {
    draws(candidate) {
      const cached = known.get(candidate)
      if (cached !== undefined) return cached
      const drawn = Boolean(design.candidatesToCss([candidate])[0]?.trim())
      known.set(candidate, drawn)
      return drawn
    },
  }
}
