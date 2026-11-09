import type { components } from '@/gen/api'

type Access = components['schemas']['RoleAccess']
type ObjectAccess = components['schemas']['ObjectAccess']
type Privilege = components['schemas']['PrivilegeSource']['privilege']

/**
 * FEAT-076 AC-18. What a role may do in one database as a matrix: a row per
 * table or routine, a column per privilege, and a cell that says yes, no, or
 * on some columns. A privilege held through somebody else carries a marker,
 * and the markers are explained once under the table — so the matrix reads
 * as a grid of answers and the sources are read when they are wanted.
 */

export type Cell =
  | { held: 'no' }
  | { held: 'yes'; marker: number | null }
  | { held: 'columns'; marker: number; columns: string[] }

export type MatrixRow = { name: string; kind: string; cells: Partial<Record<Privilege, Cell>> }

export type Footnote = { marker: number; text: string }

export type Matrix = { privileges: Privilege[]; rows: MatrixRow[]; footnotes: Footnote[] }

const always: Privilege[] = ['SELECT', 'INSERT', 'UPDATE', 'DELETE']
const rarer: Privilege[] = ['TRUNCATE', 'REFERENCES', 'TRIGGER']

/** Numbers each distinct explanation once, in the order it is first needed. */
class Notes {
  private readonly order = new Map<string, number>()

  mark(text: string): number {
    const known = this.order.get(text)
    if (known !== undefined) return known
    const marker = this.order.size + 1
    this.order.set(text, marker)
    return marker
  }

  list(): Footnote[] {
    return [...this.order].map(([text, marker]) => ({ marker, text }))
  }
}

function sourceText(via: readonly string[], owner: string): string {
  return `via ${via.map((name) => (name === owner ? `${name} (owner)` : name)).join(', ')}`
}

function cellFor(object: ObjectAccess, privilege: Privilege, role: string, notes: Notes): Cell {
  const held = object.privileges.find((entry) => entry.privilege === privilege)
  if (held !== undefined) {
    if (held.via.includes(role)) return { held: 'yes', marker: null }
    return { held: 'yes', marker: notes.mark(sourceText(held.via, object.owner)) }
  }
  const onColumns = object.columns.find((entry) => entry.privilege === privilege)
  if (onColumns !== undefined) {
    const source = onColumns.via.includes(role)
      ? 'directly'
      : sourceText(onColumns.via, object.owner)
    const text = `${privilege} on ${object.name} (${onColumns.columns.join(', ')}) ${source}`
    return { held: 'columns', marker: notes.mark(text), columns: onColumns.columns }
  }
  return { held: 'no' }
}

function matrix(
  objects: readonly ObjectAccess[],
  privileges: Privilege[],
  role: string,
  label: (object: ObjectAccess) => string,
): Matrix {
  const notes = new Notes()
  const rows = objects.map((object) => {
    const cells: Partial<Record<Privilege, Cell>> = {}
    for (const privilege of privileges) cells[privilege] = cellFor(object, privilege, role, notes)
    return { name: label(object), kind: object.kind, cells }
  })
  return { privileges, rows, footnotes: notes.list() }
}

/** The tables, with the rarer privileges as columns only where the role holds one. */
export function tableMatrix(access: Access): Matrix {
  const present = new Set(
    access.tables.flatMap((table) => [
      ...table.privileges.map((entry) => entry.privilege),
      ...table.columns.map((entry) => entry.privilege),
    ]),
  )
  const privileges = [...always, ...rarer.filter((privilege) => present.has(privilege))]
  return matrix(access.tables, privileges, access.role, (table) => `${table.schema}.${table.name}`)
}

export function routineMatrix(access: Access): Matrix {
  return matrix(
    access.functions,
    ['EXECUTE'],
    access.role,
    (routine) => `${routine.schema}.${routine.name}`,
  )
}

/** The owner's one line, and its tables named only while that stays short. */
export function ownership(access: Access): { count: number; tables: string[] | null } {
  const owned = access.tables.filter((table) => table.owned)
  return {
    count: owned.length,
    tables: owned.length <= 10 ? owned.map((table) => `${table.schema}.${table.name}`) : null,
  }
}

const superscripts = ['⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹']

/** A marker as a superscript numeral: 1 is ¹, 12 is ¹². */
export function superscript(marker: number): string {
  return String(marker)
    .split('')
    .map((digit) => superscripts[Number(digit)] ?? digit)
    .join('')
}
