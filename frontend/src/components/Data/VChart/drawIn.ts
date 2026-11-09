import { ref, watch, type Ref } from 'vue'

export function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

/**
 * Whether a chart plays its draw-in now: once, the first time it has data,
 * and never under reduced motion. The caller calls `done` when the animation
 * ends, so a later refresh that re-creates the marks does not replay it.
 */
export function useDrawIn(hasData: () => boolean): { playing: Ref<boolean>; done: () => void } {
  const playing = ref(false)
  let played = false
  watch(
    hasData,
    (present) => {
      if (!present || played) return
      played = true
      playing.value = !prefersReducedMotion()
    },
    { immediate: true },
  )
  return {
    playing,
    done: () => {
      playing.value = false
    },
  }
}
