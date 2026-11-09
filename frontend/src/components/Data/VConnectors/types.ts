/** One line's two ends, in the container's own pixels. */
export type Connector = {
  key: string
  x1: number
  y1: number
  x2: number
  y2: number
  /** Drawn dashed, for a relation that is weaker than the solid one. */
  dashed?: boolean
  /**
   * Where the line turns. Halfway by default; a caller with several lines
   * leaving different boxes gives each box its own lane, so lines from two
   * boxes never share a vertical run and read as joined to each other.
   */
  turn?: number
  /** Drawn heavier, or faded, while the caller points at one box. */
  emphasis?: 'lit' | 'dim'
}
