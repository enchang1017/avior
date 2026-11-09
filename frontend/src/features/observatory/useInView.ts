import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/**
 * Whether an element has come near the viewport, once. Without an
 * IntersectionObserver the answer is yes at mount, so nothing is left unrendered.
 */
export function useInView(target: Ref<Element | null>): Ref<boolean> {
  const seen = ref(false)
  let observer: IntersectionObserver | null = null

  onMounted(() => {
    if (typeof IntersectionObserver === 'undefined' || target.value === null) {
      seen.value = true
      return
    }
    observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return
      seen.value = true
      observer?.disconnect()
      observer = null
    })
    observer.observe(target.value)
  })

  onBeforeUnmount(() => observer?.disconnect())

  return seen
}
