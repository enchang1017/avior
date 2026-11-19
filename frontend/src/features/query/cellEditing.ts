import type { ColumnRef } from '@/shared/api'

export function cellKind(
  column: ColumnRef,
): 'text' | 'number' | 'boolean' | 'enum' | 'date' | 'time' | 'timestamp' | 'json' {
  if (column.enumValues?.length) return 'enum'
  const type = column.type.toLowerCase()
  if (type.endsWith('[]')) return 'text'
  if (/^(smallint|integer|bigint|numeric|decimal|real|double precision)(?:\(|$)/u.test(type))
    return 'number'
  if (type === 'boolean') return 'boolean'
  if (type === 'date') return 'date'
  if (type.startsWith('timestamp')) return 'timestamp'
  if (type.startsWith('time')) return 'time'
  if (/^jsonb?$/u.test(type)) return 'json'
  return 'text'
}
export function inlineCell(column: ColumnRef, value: string | null): boolean {
  if (column.type.endsWith('[]')) return false
  return (
    (cellKind(column) === 'number' ||
      /^(text|character(?: varying)?)(?:\(|$)/u.test(column.type)) &&
    (value?.length ?? 0) <= 64 &&
    !/[\r\n]/u.test(value ?? '')
  )
}
export function cellHint(column: ColumnRef): string {
  const kind = cellKind(column)
  if (kind === 'number') return 'All digits, decimal places and exponents are preserved.'
  if (kind === 'timestamp' || kind === 'time')
    return column.type.includes('with time zone')
      ? 'Include Z or a UTC offset such as +08:00. Fractional seconds are preserved; no timezone is inferred.'
      : 'Wall-clock time without a timezone. Fractional seconds are preserved; no timezone conversion is applied.'
  if (kind === 'date')
    return 'Calendar date, without a timezone. PostgreSQL date text is preserved.'
  if (kind === 'enum') return 'Choose one of the exact values declared by PostgreSQL.'
  return 'NULL is different from an empty string or the text "NULL".'
}
export function validateCell(column: ColumnRef, value: string | null): string {
  if (value === null) return column.nullable ? '' : 'This column does not accept NULL.'
  if (value.length > 65536 || value.includes('\0'))
    return 'The value is too large or contains a null character.'
  const kind = cellKind(column)
  if (kind === 'enum' && !column.enumValues?.includes(value)) return 'Choose a declared enum value.'
  if (kind === 'boolean' && !['true', 'false'].includes(value)) return 'Choose true or false.'
  if (kind === 'number') {
    const integer = /^(smallint|integer|bigint)$/u.test(column.type)
    if (
      !(
        integer
          ? /^[+-]?\d+$/u
          : /^(?:[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?|NaN|[+-]?Infinity)$/iu
      ).test(value.trim())
    )
      return integer ? 'Enter a whole number or choose NULL.' : 'Enter a number or choose NULL.'
  }
  if (
    (kind === 'timestamp' || kind === 'time') &&
    column.type.includes('with time zone') &&
    !/(?:Z|[+-]\d{2}(?::?\d{2})?)$/iu.test(value.trim())
  )
    return 'Add Z or an explicit UTC offset; Avior does not infer your timezone.'
  if (['date', 'time', 'timestamp'].includes(kind) && !value.trim())
    return 'Enter a date/time or choose NULL.'
  return ''
}
