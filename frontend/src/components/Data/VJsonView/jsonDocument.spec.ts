import { describe, expect, it } from 'vitest'
import { arrayDocument, jsonDocument, pointer, selectedJSON } from './jsonDocument'

describe('exact structured values', () => {
  it('formats JSON without rounding integers, decimal precision or exponents', () => {
    const raw = '{"id":9007199254740993,"amount":1.2300,"large":1e400,"ok":false}'
    const document = jsonDocument(raw)
    expect(document.valid).toBe(true)
    if (!document.valid) throw new Error('JSON must parse')
    expect(document.formatted).toContain('9007199254740993')
    expect(document.formatted).toContain('1.2300')
    expect(document.formatted).toContain('1e400')
    expect(selectedJSON(document.value, ['id'])).toBe('9007199254740993')
    expect(selectedJSON(document.value, ['ok'])).toBe('false')
    expect(selectedJSON(document.value, ['missing'])).toBe('')
    expect(selectedJSON(null, ['missing'])).toBe('')
  })
  it('preserves invalid source as a failed parse', () => {
    expect(jsonDocument('{bad')).toEqual({ valid: false })
  })
  it('uses escaped JSON Pointers rather than ambiguous dot paths', () => {
    expect(pointer(['a/b', '~c', 0])).toBe('/a~1b/~0c/0')
    expect(pointer([])).toBe('')
  })
  it('reads PostgreSQL arrays without converting their numeric strings', () => {
    const document = arrayDocument('{9007199254740993,NULL,"NULL","a,b"}')
    expect(document.valid).toBe(true)
    if (!document.valid) throw new Error('array must parse')
    expect(document.value).toEqual(['9007199254740993', null, 'NULL', 'a,b'])
    expect(arrayDocument('[2:3]={a,b}').valid).toBe(true)
    expect(arrayDocument('[[1], [2]]').valid).toBe(true)
    expect(arrayDocument('{{a,b},{c,d}}').valid).toBe(true)
    expect(arrayDocument('not an array').valid).toBe(false)
    expect(arrayDocument('{unbalanced').valid).toBe(false)
  })
})
