import { describe, expect, it } from 'vitest'

import { cellValue } from './cellValue'

describe('cell presentation', () => {
  it.each([
    0,
    false,
    true,
    42,
    'alice',
    '9007199254740993',
    '123.4500',
    '2026-11-19T11:00:00+08:00',
    '550e8400-e29b-41d4-a716-446655440000',
  ])('keeps a scalar unchanged: %s', (value) => {
    expect(cellValue(value)).toMatchObject({
      raw: String(value),
      preview: String(value),
      expandable: false,
      copyable: true,
    })
  })

  it('distinguishes NULL, empty strings, whitespace and their literal labels', () => {
    expect(cellValue(null)).toMatchObject({ preview: 'NULL', copyable: false, expandable: false })
    expect(cellValue('')).toMatchObject({
      preview: 'EMPTY',
      raw: '',
      copyable: true,
      expandable: false,
    })
    expect(cellValue(' \t\n')).toMatchObject({
      preview: 'WHITESPACE',
      raw: ' \t\n',
      expandable: true,
    })
    for (const text of ['NULL', 'EMPTY', 'WHITESPACE']) expect(cellValue(text).special).toBe(false)
  })

  it('bounds the preview while retaining the complete value', () => {
    const value = 'x'.repeat(10000)
    expect(cellValue(value)).toMatchObject({ raw: value, detail: value, expandable: true })
    expect(cellValue(value).preview).toHaveLength(65)
  })

  it('does not split emoji or abbreviate a short Unicode value', () => {
    expect(cellValue('🙂'.repeat(40)).expandable).toBe(false)
    expect(cellValue('a'.repeat(63) + '🙂tail').preview).toBe('a'.repeat(63) + '🙂…')
  })

  it('offers a multiline detail without changing line endings or tabs', () => {
    const value = 'first\r\nsecond\tcolumn'
    expect(cellValue(value)).toMatchObject({
      preview: 'first  second column…',
      raw: value,
      detail: value,
      expandable: true,
    })
  })

  it('formats structured API values for reading but copies their compact representation', () => {
    expect(cellValue({ a: [1, null] })).toMatchObject({
      raw: '{"a":[1,null]}',
      detail: '{\n  "a": [\n    1,\n    null\n  ]\n}',
      expandable: true,
    })
    expect(cellValue([]).expandable).toBe(true)
  })

  it('does not parse JSON strings and round large numbers or normalize their source', () => {
    const raw = '{ "id": 9007199254740993 }'
    expect(cellValue(raw, { name: 'payload', type: 'JSONB' })).toMatchObject({
      raw,
      detail: raw,
      expandable: true,
    })
    expect(cellValue('not valid JSON', { name: 'payload', type: 'json' }).raw).toBe(
      'not valid JSON',
    )
    for (const raw of ['42', 'false', '"short"']) {
      expect(cellValue(raw, { name: 'payload', type: 'jsonb' }).expandable).toBe(false)
    }
  })

  it('never exposes or offers to copy a masked value', () => {
    const value = cellValue('hidden secret', { name: 'email', type: 'text', masked: true })
    expect(value).toMatchObject({
      preview: '***',
      raw: '***',
      detail: '***',
      copyable: false,
      expandable: false,
    })
    expect(JSON.stringify(value)).not.toContain('hidden secret')
  })

  it('treats HTML, URLs and binary representations as inert API text', () => {
    for (const raw of ['<script>alert(1)</script>', 'https://example.test', '\\xdeadbeef']) {
      expect(cellValue(raw).raw).toBe(raw)
    }
  })
})
