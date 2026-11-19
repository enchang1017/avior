import type { QueryResult } from '@/shared/api'

type Column = QueryResult['columns'][number]

/** Presentation never changes the value copied from the API. */
export function cellValue(value: unknown, column?: Column) {
  const masked = column?.masked === true
  const absent = value === null || value === undefined
  const raw = masked
    ? '***'
    : absent
      ? ''
      : typeof value === 'string'
        ? value
        : typeof value === 'object'
          ? JSON.stringify(value)
          : typeof value === 'number' || typeof value === 'boolean' || typeof value === 'bigint'
            ? value.toString()
            : (JSON.stringify(value) ?? '')
  const empty = !masked && !absent && raw === ''
  const whitespace = !masked && !absent && !empty && raw.trim() === ''
  const structured =
    !masked &&
    !absent &&
    (typeof value === 'object' || (/^jsonb?$/iu.test(column?.type ?? '') && /^\s*[[{]/u.test(raw)))
  const multiline = /[\r\n\t]/u.test(raw)
  const flattened = raw.replace(/[\r\n\t]/gu, ' ')
  // Bound the work and avoid cutting a surrogate pair in a Unicode value.
  const prefix = Array.from(flattened.slice(0, 130)).slice(0, 64).join('')
  const shortened = prefix.length < flattened.length
  const expandable =
    !masked && !absent && !empty && (shortened || multiline || structured || whitespace)
  const kind = masked
    ? 'Masked'
    : absent
      ? 'NULL'
      : empty
        ? 'Empty string'
        : whitespace
          ? 'Whitespace'
          : structured
            ? 'JSON / array'
            : 'Value'
  const preview = masked
    ? '***'
    : absent
      ? 'NULL'
      : empty
        ? 'EMPTY'
        : whitespace
          ? 'WHITESPACE'
          : prefix + (shortened || multiline ? '…' : '')
  return {
    raw,
    preview,
    get detail() {
      return structured && typeof value === 'object' ? JSON.stringify(value, null, 2) : raw
    },
    kind,
    expandable,
    copyable: !masked && !absent,
    special: masked || absent || empty || whitespace,
  }
}
