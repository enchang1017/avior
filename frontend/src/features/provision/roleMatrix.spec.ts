import { describe, expect, it } from 'vitest'

import type { components } from '@/gen/api'
import { ownership, routineMatrix, superscript, tableMatrix } from './roleMatrix'

type Access = components['schemas']['RoleAccess']
type ObjectAccess = components['schemas']['ObjectAccess']

const table = (name: string, extra: Partial<ObjectAccess> = {}): ObjectAccess => ({
  schema: 'public',
  name,
  kind: 'r',
  owner: 'shop_schema_owner',
  owned: false,
  privileges: [],
  columns: [],
  ...extra,
})

const access = (extra: Partial<Access> = {}): Access => ({
  role: 'shop_ops',
  database: 'shop',
  canConnect: true,
  connectVia: ['role_shop'],
  connectable: [],
  tables: [],
  functions: [],
  truncated: false,
  ...extra,
})

/*
 * FEAT-076 AC-18. The matrix the role dialog draws: a cell per table and
 * privilege, and a footnote for every source that is not the role itself.
 */
describe('tableMatrix', () => {
  const matrix = tableMatrix(
    access({
      tables: [
        table('bet', {
          privileges: [
            { privilege: 'SELECT', via: ['shop_ops'] },
            { privilege: 'INSERT', via: ['role_shop'] },
          ],
        }),
        table('round', {
          privileges: [
            { privilege: 'SELECT', via: ['role_shop'] },
            { privilege: 'UPDATE', via: ['role_report', 'role_shop'] },
          ],
          columns: [{ privilege: 'INSERT', columns: ['id', 'note'], via: ['role_report'] }],
        }),
        table('quiet'),
      ],
    }),
  )
  const cell = (row: string, privilege: 'SELECT' | 'INSERT' | 'UPDATE' | 'DELETE') =>
    matrix.rows.find((entry) => entry.name === `public.${row}`)?.cells[privilege]

  it('has the four privileges as columns when nothing rarer is held', () => {
    expect(matrix.privileges).toEqual(['SELECT', 'INSERT', 'UPDATE', 'DELETE'])
  })

  // Markers are numbered in reading order: row by row, left to right.
  it('marks a direct grant plainly and an inherited one with its footnote', () => {
    expect(cell('bet', 'SELECT')).toEqual({ held: 'yes', marker: null })
    expect(cell('bet', 'INSERT')).toEqual({ held: 'yes', marker: 1 })
    expect(cell('round', 'SELECT')).toEqual({ held: 'yes', marker: 1 })
    expect(cell('round', 'UPDATE')).toEqual({ held: 'yes', marker: 3 })
    expect(cell('bet', 'DELETE')).toEqual({ held: 'no' })
  })

  it('says "on some columns" and names them in the footnote', () => {
    expect(cell('round', 'INSERT')).toEqual({ held: 'columns', marker: 2, columns: ['id', 'note'] })
    expect(matrix.footnotes).toEqual([
      { marker: 1, text: 'via role_shop' },
      { marker: 2, text: 'INSERT on round (id, note) via role_report' },
      { marker: 3, text: 'via role_report, role_shop' },
    ])
  })

  it('keeps a row for a table the role holds nothing on, every cell no', () => {
    const quiet = matrix.rows.find((row) => row.name === 'public.quiet')
    expect(Object.values(quiet?.cells ?? {}).every((entry) => entry.held === 'no')).toBe(true)
  })

  it('adds a rarer privilege as a column only when the role holds it', () => {
    const rarer = tableMatrix(
      access({
        tables: [table('bet', { privileges: [{ privilege: 'TRUNCATE', via: ['shop_ops'] }] })],
      }),
    )
    expect(rarer.privileges).toEqual(['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE'])
  })

  it('names the owner as the source of what ownership gives', () => {
    const owned = tableMatrix(
      access({
        tables: [
          table('bet', {
            owned: true,
            privileges: [{ privilege: 'SELECT', via: ['shop_schema_owner'] }],
          }),
        ],
      }),
    )
    expect(owned.footnotes).toEqual([{ marker: 1, text: 'via shop_schema_owner (owner)' }])
  })
})

describe('routineMatrix', () => {
  it('has EXECUTE alone, marked where it comes from PUBLIC', () => {
    const matrix = routineMatrix(
      access({
        functions: [
          table('settle(n integer)', {
            kind: 'function',
            privileges: [{ privilege: 'EXECUTE', via: ['PUBLIC'] }],
          }),
          table('purge()', { kind: 'function' }),
        ],
      }),
    )
    expect(matrix.privileges).toEqual(['EXECUTE'])
    expect(matrix.rows.map((row) => row.cells.EXECUTE)).toEqual([
      { held: 'yes', marker: 1 },
      { held: 'no' },
    ])
    expect(matrix.footnotes).toEqual([{ marker: 1, text: 'via PUBLIC' }])
  })
})

describe('ownership', () => {
  it('counts what the owner owns and names it while that is ten or fewer', () => {
    const few = ownership(access({ tables: [table('bet', { owned: true }), table('round')] }))
    expect(few).toEqual({ count: 1, tables: ['public.bet'] })
    const many = ownership(
      access({ tables: Array.from({ length: 11 }, (_, i) => table(`t${i}`, { owned: true })) }),
    )
    expect(many).toEqual({ count: 11, tables: null })
  })
})

describe('superscript', () => {
  it('writes a marker in superscript numerals', () => {
    expect(superscript(1)).toBe('¹')
    expect(superscript(12)).toBe('¹²')
  })
})
