import { describe, expect, it } from 'vitest'

import { hasComments, withoutComments } from './sqlComments'

describe('withoutComments', () => {
  it('drops a line comment and keeps the statement under it', () => {
    expect(withoutComments('-- why this exists\nSELECT 1;')).toBe('SELECT 1;')
  })

  // The newline stays, or the next statement is pulled onto the line before it
  // and two statements read as one.
  it('drops a trailing comment without joining the lines', () => {
    expect(withoutComments('SELECT 1; -- first\nSELECT 2;')).toBe('SELECT 1;\nSELECT 2;')
  })

  it('drops a block comment', () => {
    expect(withoutComments('/* header\n   over lines */\nSELECT 1;')).toBe('SELECT 1;')
  })

  // PostgreSQL nests these; C does not. Counting only the first `*/` would
  // leave the tail of the comment on the page as though it were SQL.
  it('drops a nested block comment whole', () => {
    expect(withoutComments('/* outer /* inner */ still outer */ SELECT 1;')).toBe('SELECT 1;')
  })

  /*
   * The reason this is a scan and not a substitution.
   *
   * Every case below contains something that looks like a comment and is not.
   * Cutting at it would change the SQL an approver is reading from the SQL that
   * runs, which is worse than showing them the prose.
   */
  it('leaves what only looks like a comment inside a string', () => {
    const sql = "SELECT 'a -- b', 'c /* d */ e';"
    expect(withoutComments(sql)).toBe(sql)
  })

  it('leaves a comment marker inside a quoted identifier', () => {
    const sql = 'SELECT "odd -- name" FROM t;'
    expect(withoutComments(sql)).toBe(sql)
  })

  it('reads a doubled quote as an escape, not as the end of the string', () => {
    const sql = "SELECT 'it''s -- fine';"
    expect(withoutComments(sql)).toBe(sql)
  })

  it('reads a backslash escape as an escape', () => {
    const sql = "SELECT E'a\\' -- still in the string';"
    expect(withoutComments(sql)).toBe(sql)
  })

  // A function body is dollar-quoted and is full of comments that belong to it.
  // They are part of the object being created, not prose around it.
  it('leaves a dollar-quoted body alone, comments and all', () => {
    const sql = ['CREATE FUNCTION f() RETURNS int AS $$', '  -- kept', '  SELECT 1;', '$$;'].join(
      '\n',
    )
    expect(withoutComments(sql)).toBe(sql)
  })

  it('leaves a tagged dollar-quoted body alone', () => {
    const sql = ['CREATE FUNCTION f() RETURNS int AS $body$', '  -- kept', '$body$;'].join('\n')
    expect(withoutComments(sql)).toBe(sql)
  })

  // `$1` is a parameter, not the start of a dollar-quoted string. Reading it as
  // one would swallow the rest of the file.
  it('does not read a positional parameter as a quote', () => {
    expect(withoutComments('SELECT $1; -- gone')).toBe('SELECT $1;')
  })

  it('collapses the blank lines the cut leaves behind', () => {
    const sql = ['-- one', '', '-- two', '', 'SELECT 1;', '', '-- trailing'].join('\n')
    expect(withoutComments(sql)).toBe('SELECT 1;')
  })

  it('leaves the blank line that separates two statements', () => {
    expect(withoutComments('SELECT 1;\n\nSELECT 2;')).toBe('SELECT 1;\n\nSELECT 2;')
  })

  it('survives a comment nobody closed', () => {
    expect(withoutComments('SELECT 1;\n/* never closed')).toBe('SELECT 1;')
  })

  it('survives a string nobody closed', () => {
    expect(withoutComments("SELECT 'never closed")).toBe("SELECT 'never closed")
  })
})

describe('hasComments', () => {
  it('is false when there is nothing to hide', () => {
    expect(hasComments('SELECT 1;')).toBe(false)
  })

  it('is true when a comment would be removed', () => {
    expect(hasComments('-- a\nSELECT 1;')).toBe(true)
  })

  // Otherwise the control appears on a file whose only comment is inside a
  // function body, and pressing it does nothing.
  it('is false when the only comment belongs to a dollar-quoted body', () => {
    expect(hasComments('CREATE FUNCTION f() RETURNS int AS $$\n -- kept\n$$;')).toBe(false)
  })
})
