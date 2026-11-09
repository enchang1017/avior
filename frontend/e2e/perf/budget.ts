/*
 * ADR 0009's runtime budget, P8 to P18, in one place the performance specs and
 * slice 3's effects spec both read. None has a tolerance.
 *
 * P15 is the user's decision of 2026-09-26: as much motion as performance
 * allows. The cap is the highest count of infinite animations measured with
 * zero Layout, zero Paint and every frame within 20 ms, at 4x CPU on the full
 * production shell: 24. The gate fails at 25.
 */
export const budget = {
  lcpMs: 1200,
  keystrokeReadyMs: 1500,
  cls: 0.02,
  longTaskMs: 50,
  paletteKeystrokeMs: 100,
  quietWindowMs: 5000,
  smoothFrameMs: 20,
  smoothShare: 0.95,
  gridRows: 60,
  consoleElements: 1500,
  chart: { nodes: 150, edges: 300, svgElements: 700 },
  /** P20 to P22: the lineage page (FEAT-070). */
  lineage: { nodes: 150, edges: 300, svgElements: 700, layoutMs: 16, paintMs: 150, particles: 6 },
  motion: { maxInfiniteAnimations: 24 },
  /** P3, as scripts/budget.mjs holds a route: first-load script, gzipped at level 9. */
  routeKb: 90,
}

/** An animation's name without the hash a scoped keyframe carries: `ring-1a2b3c4d` is `ring`. */
export function animationName(name: string): string {
  return name.replace(/-[0-9a-f]{8}$/u, '')
}
