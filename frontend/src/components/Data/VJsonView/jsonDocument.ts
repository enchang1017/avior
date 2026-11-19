import { parse as parseArray } from 'postgres-array'
import { parse, stringify } from 'lossless-json'

/** Retain number tokens rather than passing them through JavaScript Number. */
export const exactJSON = { parse: (text: string): unknown => parse(text), stringify }

export function jsonDocument(
  text: string,
): { valid: true; value: unknown; formatted: string } | { valid: false } {
  try {
    const value = exactJSON.parse(text)
    return { valid: true, value, formatted: stringify(value, null, 2) ?? text }
  } catch {
    return { valid: false }
  }
}

export function pointer(path: (string | number)[]): string {
  return path
    .map((segment) => '/' + String(segment).replaceAll('~', '~0').replaceAll('/', '~1'))
    .join('')
}

export function selectedJSON(value: unknown, path: (string | number)[]): string {
  let current = value
  for (const segment of path) {
    if (
      current === null ||
      typeof current !== 'object' ||
      !Object.prototype.hasOwnProperty.call(current, segment)
    )
      return ''
    current = (current as Record<string | number, unknown>)[segment]
  }
  return stringify(current) ?? ''
}

export function arrayDocument(text: string): ReturnType<typeof jsonDocument> {
  const json = jsonDocument(text)
  if (json.valid && Array.isArray(json.value)) return json
  try {
    if (!/^\s*(?:\[[^=]+\]=)?\{/u.test(text)) return { valid: false }
    const value = parseArray(text)
    return { valid: true, value, formatted: stringify(value, null, 2) ?? text }
  } catch {
    return { valid: false }
  }
}
