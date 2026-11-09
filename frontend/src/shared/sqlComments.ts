/**
 * Strips comments from SQL, for reading rather than for running.
 *
 * A migration file opens with a paragraph explaining itself, and an approver
 * scanning ten of them is reading prose to find the statements. This removes
 * the prose. It never reaches the server: what is applied is the file as it was
 * submitted, and the page can always show that.
 *
 * Written as a scan rather than a regular expression because `--` and `/*` mean
 * nothing inside a string, and a substitution that does not know where the
 * strings are will cut a statement in half. The literals that hide them:
 *
 *   'text'        doubled quote escapes, and E'\\' backslash escapes
 *   "identifier"  doubled quote escapes
 *   $$ ... $$     dollar quoting, with an optional tag, ends only on its match
 *
 * and PostgreSQL nests block comments, which C does not.
 */
export function withoutComments(sql: string): string {
  let out = ''
  let index = 0

  while (index < sql.length) {
    const rest = sql.slice(index)

    if (rest.startsWith('--')) {
      const end = sql.indexOf('\n', index)
      // The newline is kept: the statement after a trailing comment must not
      // be pulled onto the line before it.
      index = end === -1 ? sql.length : end
      continue
    }

    if (rest.startsWith('/*')) {
      index = endOfBlockComment(sql, index)
      // A comment that opened the line takes the space after it too, or the
      // statement it introduced is left indented by however wide the comment's
      // punctuation was.
      if (out === '' || out.endsWith('\n')) {
        while (sql[index] === ' ' || sql[index] === '\t') index += 1
      }
      continue
    }

    const dollar = dollarTagAt(sql, index)
    if (dollar !== null) {
      const close = sql.indexOf(dollar, index + dollar.length)
      const end = close === -1 ? sql.length : close + dollar.length
      out += sql.slice(index, end)
      index = end
      continue
    }

    if (rest.startsWith("'") || rest.startsWith('"')) {
      const end = endOfQuoted(sql, index, rest[0] as string)
      out += sql.slice(index, end)
      index = end
      continue
    }

    out += sql[index]
    index += 1
  }

  return tidy(out)
}

/** Nested, the way PostgreSQL counts them. */
function endOfBlockComment(sql: string, start: number): number {
  let depth = 0
  let index = start
  while (index < sql.length) {
    if (sql.startsWith('/*', index)) {
      depth += 1
      index += 2
      continue
    }
    if (sql.startsWith('*/', index)) {
      depth -= 1
      index += 2
      if (depth === 0) return index
      continue
    }
    index += 1
  }
  return sql.length
}

/**
 * The end of a quoted literal, past the closing quote.
 *
 * A doubled quote is an escaped one and does not close the literal. So is a
 * backslash-escaped one, which is only meaningful in an E'' string but is
 * treated as an escape everywhere: reading one quote too many leaves the rest
 * of the file inside a string, which is the failure that matters.
 */
function endOfQuoted(sql: string, start: number, quote: string): number {
  let index = start + 1
  while (index < sql.length) {
    if (sql[index] === '\\') {
      index += 2
      continue
    }
    if (sql[index] === quote) {
      if (sql[index + 1] === quote) {
        index += 2
        continue
      }
      return index + 1
    }
    index += 1
  }
  return sql.length
}

/** `$$` or `$tag$` at this position, or null. Not `$1`, which is a parameter. */
function dollarTagAt(sql: string, index: number): string | null {
  if (sql[index] !== '$') return null
  const match = /^\$[A-Za-z_][A-Za-z0-9_]*\$|^\$\$/u.exec(sql.slice(index))
  return match === null ? null : match[0]
}

/**
 * Removes what the cut left behind: lines that held only a comment, and the
 * runs of blank lines that separated paragraphs of them.
 */
function tidy(sql: string): string {
  const lines = sql.split('\n')
  const kept: string[] = []
  for (const line of lines) {
    const blank = line.trim() === ''
    if (blank && (kept.length === 0 || kept[kept.length - 1]?.trim() === '')) continue
    kept.push(line.trimEnd())
  }
  while (kept.length > 0 && kept[kept.length - 1]?.trim() === '') kept.pop()
  return kept.join('\n')
}

/** Whether stripping would change anything, so a control is offered only when
 * it would do something. */
export function hasComments(sql: string): boolean {
  return withoutComments(sql) !== tidy(sql)
}
