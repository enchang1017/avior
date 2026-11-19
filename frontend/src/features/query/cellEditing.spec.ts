import { describe, expect, it } from 'vitest'
import { cellHint, cellKind, inlineCell, validateCell } from './cellEditing'
import type { ColumnRef } from '@/shared/api'
const field = (type: string, extra: Partial<ColumnRef> = {}): ColumnRef => ({
  name: 'value',
  type,
  nullable: true,
  denied: false,
  ...extra,
})
describe('precise typed cell drafts', () => {
  it('uses inline editing only for short scalar text and numbers', () => {
    for (const type of ['text', 'character varying(40)', 'bigint', 'numeric(30,12)'])
      expect(inlineCell(field(type), '42')).toBe(true)
    for (const type of ['jsonb', 'text[]', 'character varying(20)[]', 'boolean', 'uuid', 'date'])
      expect(inlineCell(field(type), '42')).toBe(false)
    expect(inlineCell(field('text'), 'x'.repeat(65))).toBe(false)
    expect(inlineCell(field('text'), 'a\nb')).toBe(false)
    expect(inlineCell(field('text'), null)).toBe(true)
  })
  it('keeps bigint, numeric exponents and NULL distinct without Number conversion', () => {
    for (const value of ['9007199254740993', '-900719925474099312345'])
      expect(validateCell(field('bigint'), value)).toBe('')
    for (const value of ['1.0000000000000000001', '2.5e-200', 'NaN', '-Infinity'])
      expect(validateCell(field('numeric'), value)).toBe('')
    expect(validateCell(field('bigint'), '1.5')).toContain('whole number')
    expect(validateCell(field('numeric'), '')).toContain('number')
    expect(validateCell(field('text'), '')).toBe('')
    expect(validateCell(field('text'), 'NULL')).toBe('')
    expect(validateCell(field('text'), null)).toBe('')
    expect(validateCell(field('text', { nullable: false }), null)).toContain('does not accept NULL')
  })
  it('selects exact enum labels and booleans, including empty and literal NULL labels', () => {
    const enumField = field('public.state', { enumValues: ['', 'NULL', "needs' review"] })
    expect(cellKind(enumField)).toBe('enum')
    for (const value of enumField.enumValues ?? []) expect(validateCell(enumField, value)).toBe('')
    expect(validateCell(enumField, 'null')).toContain('declared')
    expect(validateCell(field('boolean'), 'false')).toBe('')
    expect(validateCell(field('boolean'), 'yes')).toContain('true or false')
  })
  it('requires an explicit timezone without converting or truncating fractional seconds', () => {
    const zoned = field('timestamp(6) with time zone')
    expect(validateCell(zoned, '2026-11-19 12:30:12.123456+08:00')).toBe('')
    expect(validateCell(zoned, '2026-11-19T12:30:12.123456Z')).toBe('')
    expect(validateCell(zoned, '2026-11-19 12:30:12')).toContain('offset')
    expect(validateCell(field('timestamp without time zone'), '2026-11-19 12:30:12.123456')).toBe(
      '',
    )
    expect(cellHint(zoned)).toContain('no timezone is inferred')
    expect(cellKind(field('time with time zone'))).toBe('time')
    expect(cellKind(field('date'))).toBe('date')
    expect(validateCell(field('date'), '')).toContain('date/time')
    expect(validateCell(field('text'), '\0')).toContain('null character')
  })
})
