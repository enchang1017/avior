export type StarShape = 'table' | 'view' | 'matview' | 'function' | 'procedure' | 'sequence'

/** How strongly a star is drawn: the focus, what is one hop from it, and the rest. */
export type Tier = 'focus' | 'near' | 'far'

export type ChartStar = {
  id: string
  shape: StarShape
  /** The name drawn beside the star: a routine's without its arguments. */
  label: string
  /** The name in full, for the tooltip. */
  title: string
  x: number
  y: number
  tier: Tier
  labelled: boolean
  /** Which side of the star its label sits on, so labels point away from the centre. */
  side: 'left' | 'right'
  /** A collapsed hub: the neighbours it hides. */
  hidden: number
  /** How many drawn edges touch it, which sets its magnitude. */
  degree: number
  extension: boolean
  /** It builds SQL at run time, or calls something that does. */
  badge: StarBadge
}

export type StarBadge = 'dynamic' | 'may_reach_unknown' | null

/** Read and write are the only two edge colours; everything else is neutral. */
export type EdgeTone = 'read' | 'write' | 'reference'

export type EdgeMark = 'foreign_key' | 'trigger' | null

export type ChartEdge = {
  key: string
  from: string
  to: string
  x1: number
  y1: number
  x2: number
  y2: number
  tone: EdgeTone
  parsed: boolean
  mark: EdgeMark
  far: boolean
  /** Its place along the chosen route, when it is on it. */
  step: number | null
  /** It reads or writes the column the panel is open on. */
  highlighted: boolean
  /** What it is, in words, for the hover label. */
  caption: string
}

export type ChartScene = {
  stars: ChartStar[]
  edges: ChartEdge[]
  rings: number[]
  /** Where the target ring is drawn, if anywhere. */
  ring: { x: number; y: number } | null
}

export const sceneWidth = 1000
export const sceneHeight = 640
/** Rings are ellipses, wider than tall, to fill a page that is. */
export const stretch = 1.6
export const maxParticles = 6
