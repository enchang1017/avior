import type { components } from '@/gen/api'

type Schemas = components['schemas']

export type LineageGraph = Schemas['LineageGraph']
export type LineageNode = Schemas['LineageNode']
export type LineageEdge = Schemas['LineageEdge']
export type LineageNodeKind = Schemas['LineageNodeKind']
export type LineageFocusKind = Schemas['LineageFocusKind']
export type LineageEdgeKind = Schemas['LineageEdgeKind']
export type LineageConfidence = Schemas['LineageConfidence']
export type LineagePath = Schemas['LineagePath']
export type LineageRoute = Schemas['LineageRoute']
export type LineageColumn = Schemas['LineageColumn']
export type LineageColumnEntry = Schemas['LineageColumnEntry']
export type LineageRoutine = Schemas['LineageRoutine']
export type LineageSnapshot = Schemas['LineageSnapshot']
export type LineageColumnName = Schemas['LineageColumnName']

export const nodeKinds: readonly LineageNodeKind[] = [
  'table',
  'view',
  'matview',
  'function',
  'procedure',
  'sequence',
]

export const confidences: readonly LineageConfidence[] = ['exact', 'parsed']

export const isRoutine = (kind: LineageNodeKind): boolean =>
  kind === 'function' || kind === 'procedure'

/**
 * An object named the way a person names it. The server turns it into a node
 * id, so no client reproduces the id's quoting.
 */
export type FocusRef = {
  schema: string
  name: string
  kind: LineageFocusKind
  /** A routine's identity arguments, given only when its name alone is ambiguous. */
  signature?: string
}
export type LineageOverview = Schemas['LineageOverview']
export type LineageSchemaCluster = Schemas['LineageSchemaCluster']
export type LineageSchemaLink = Schemas['LineageSchemaLink']
