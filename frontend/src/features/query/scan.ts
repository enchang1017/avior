/**
 * The scan line: one sweep over rows that are already painted, 600 ms, on the
 * compositor. Played through the Web Animations API on an element that is
 * always there, so starting it neither inserts anything nor restarts a CSS
 * animation — which would lay out or paint the page (ADR 0009 P13).
 */
export const scanMs = 600

const sweep: Keyframe[] = [
  { transform: 'translateY(-100%)', opacity: 0.9 },
  { transform: 'translateY(1400%)', opacity: 0 },
]

export function prefersLessMotion(): boolean {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

export function playScan(element: HTMLElement | null): Animation | null {
  if (element === null || prefersLessMotion() || typeof element.animate !== 'function') return null
  return element.animate(sweep, { duration: scanMs, easing: 'cubic-bezier(0.2, 0, 0, 1)' })
}
