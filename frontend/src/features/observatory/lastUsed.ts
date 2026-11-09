import type { SelectOption } from '@/components'
import { newestFirst, type ObservatoryObject, type RoutineGroup, type RoutineVersion } from './text'

export type KindFilter = '' | ObservatoryObject['kind']

export const kindOptions: SelectOption[] = [
  { value: '', label: 'Every kind' },
  { value: 'table', label: 'Tables' },
  { value: 'index', label: 'Indexes' },
  { value: 'function', label: 'Functions' },
  { value: 'procedure', label: 'Procedures' },
]

export const flagOptions: SelectOption[] = [
  { value: 'all', label: 'Every object' },
  { value: 'flagged', label: 'Flagged only' },
]

export type ObjectRow =
  | { row: 'object'; key: string; object: ObservatoryObject }
  | { row: 'group'; key: string; group: RoutineGroup }
  | { row: 'version'; key: string; object: ObservatoryObject; latest: boolean }

type Membership = { group: RoutineGroup; version: RoutineVersion }

function passes(object: ObservatoryObject, kind: string, flaggedOnly: boolean): boolean {
  return (kind === '' || object.kind === kind) && (!flaggedOnly || object.flagged)
}

export function qualifiedName(object: ObservatoryObject): string {
  const name = `${object.schema}.${object.name}`
  return object.signature === null ? name : `${name}(${object.signature})`
}

const nameKey = (schema: string, name: string) => `${schema}\u0000${name}`
const signatureKey = (schema: string, name: string, signature: string) =>
  `${nameKey(schema, name)}\u0000${signature}`

function membershipIndex(groups: readonly RoutineGroup[]) {
  const bySignature = new Map<string, Membership>()
  const byName = new Map<string, Membership>()
  for (const group of groups) {
    for (const version of group.versions) {
      const member = { group, version }
      bySignature.set(signatureKey(group.schema, version.name, version.signature), member)
      if (!byName.has(nameKey(group.schema, version.name))) {
        byName.set(nameKey(group.schema, version.name), member)
      }
    }
  }
  return (object: ObservatoryObject): Membership | undefined =>
    object.signature === null
      ? byName.get(nameKey(object.schema, object.name))
      : bySignature.get(signatureKey(object.schema, object.name, object.signature))
}

function objectFor(
  objects: readonly ObservatoryObject[],
  group: RoutineGroup,
  version: RoutineVersion,
): ObservatoryObject {
  const found = objects.find(
    (object) =>
      object.schema === group.schema &&
      object.name === version.name &&
      (object.signature === null || object.signature === version.signature),
  )
  return found ?? versionObject(group, version)
}

function versionObject(group: RoutineGroup, version: RoutineVersion): ObservatoryObject {
  return {
    schema: group.schema,
    name: version.name,
    kind: 'function',
    signature: version.signature,
    table: null,
    tracked: true,
    notTrackedReason: null,
    lastUsedAt: version.lastUsedAt,
    lastUsedKind: null,
    lastUsedNode: null,
    daysAgo: version.daysAgo,
    flagged: version.flagged,
    constraintBacked: false,
    sizeBytes: null,
    growth30dBytes: null,
    calls: null,
  }
}

function groupRows(
  objects: readonly ObservatoryObject[],
  group: RoutineGroup,
  kind: string,
  flaggedOnly: boolean,
): ObjectRow[] {
  const groupKey = `group:${nameKey(group.schema, group.base)}`
  const versions = newestFirst(group.versions)
    .map((version) => ({ version, object: objectFor(objects, group, version) }))
    .filter(({ object }) => passes(object, kind, flaggedOnly))
    .map(
      ({ version, object }): ObjectRow => ({
        row: 'version',
        key: `${groupKey}:${qualifiedName(object)}`,
        object,
        latest: version.latest,
      }),
    )
  return versions.length === 0 ? [] : [{ row: 'group', key: groupKey, group }, ...versions]
}

/**
 * The objects table's rows. A date-versioned routine is drawn inside its
 * group, placed where the group's first member would have stood, so the
 * versions read together rather than scattered through the list.
 */
export function objectRows(
  objects: readonly ObservatoryObject[],
  groups: readonly RoutineGroup[],
  kind: string,
  flaggedOnly: boolean,
): ObjectRow[] {
  const memberOf = membershipIndex(groups)
  const placed = new Set<RoutineGroup>()
  const rows: ObjectRow[] = []
  const place = (group: RoutineGroup) => {
    placed.add(group)
    rows.push(...groupRows(objects, group, kind, flaggedOnly))
  }
  for (const object of objects) {
    const member = memberOf(object)
    if (member === undefined) {
      if (passes(object, kind, flaggedOnly)) {
        rows.push({ row: 'object', key: `${object.kind}:${qualifiedName(object)}`, object })
      }
    } else if (!placed.has(member.group)) {
      place(member.group)
    }
  }
  groups.filter((group) => !placed.has(group)).forEach(place)
  return rows
}
