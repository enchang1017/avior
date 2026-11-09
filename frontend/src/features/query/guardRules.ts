/**
 * What the guard refuses on the read path, as an operator needs to know it
 * before running something: the rule the refusal will name, and what it asks.
 *
 * These are sqlguard's own rule names, so the one in this list and the one in a
 * refusal are the same string and can be matched by eye.
 */
export type GuardRule = { rule: string; asks: string }

export const readRules: readonly GuardRule[] = [
  { rule: 'statement_not_read_only', asks: 'One statement, and it only reads.' },
  { rule: 'wildcard_select', asks: 'Columns are named, never SELECT *.' },
  { rule: 'cost_limit_exceeded', asks: 'The plan costs less than the cap.' },
  { rule: 'column_denied', asks: 'Restricted columns stay closed, through views too.' },
  { rule: 'table_denied', asks: 'Tables outside your grants stay closed.' },
  { rule: 'opaque_function', asks: 'No function the guard cannot see into.' },
  { rule: 'parse_failed', asks: 'Anything the guard cannot read in full is refused.' },
]

export const writeRules: readonly GuardRule[] = [
  { rule: 'statement_not_single_write', asks: 'One UPDATE or DELETE, and nothing else.' },
  { rule: 'missing_where_clause', asks: 'A WHERE clause that narrows the rows.' },
  { rule: 'multiple_tables', asks: 'One table per request.' },
  { rule: 'row_limit_exceeded', asks: 'No more rows than the cap allows.' },
  { rule: 'irreversible_write', asks: 'Only what the backup can put back.' },
  { rule: 'column_denied', asks: 'Restricted columns stay closed.' },
]
