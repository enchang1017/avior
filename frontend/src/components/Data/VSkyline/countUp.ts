import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

export const countUpMs = 700

/** Where a count that eases out towards its target stands after `elapsed` of `duration`. */
export function countAt(target: number, elapsed: number, duration = countUpMs): number {
  if (elapsed >= duration || duration <= 0) return target
  const progress = Math.max(0, elapsed) / duration
  return Math.round(target * (1 - (1 - progress) ** 3))
}

function reduced(): boolean {
  return (
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

/**
 * A number that counts up to its value the first time it has one, then
 * follows it exactly. Only the text changes, never the SVG, and never under
 * reduced motion.
 */
export function useCountUp(value: () => number): Ref<number> {
  const shown = ref(0)
  let frame = 0
  let counted = false

  function run(target: number): void {
    // Timed from the first frame's own timestamp, never performance.now():
    // jsdom stamps frames from its window's creation, not the page's origin,
    // and a count measured across the two clocks never left zero.
    let start: number | null = null
    const step = (now: number) => {
      start ??= now
      shown.value = countAt(target, now - start)
      if (shown.value !== target) frame = requestAnimationFrame(step)
    }
    frame = requestAnimationFrame(step)
  }

  watch(
    value,
    (target) => {
      cancelAnimationFrame(frame)
      if (counted || target === 0 || reduced()) {
        shown.value = target
        return
      }
      counted = true
      run(target)
    },
    { immediate: true },
  )

  onBeforeUnmount(() => cancelAnimationFrame(frame))
  return shown
}
