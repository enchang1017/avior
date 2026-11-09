import { computed, ref, watch } from 'vue'

/**
 * A picker that is typed into. It shows what is chosen, offers what matches the
 * text once the operator types, and says so when a pick names nothing: `take`
 * acts on a pick and answers whether it named something.
 */
export function useTypedPick(
  shown: () => string,
  offer: (text: string) => string[],
  take: (text: string) => boolean,
) {
  const typed = ref(shown())
  const unresolved = ref(false)
  watch(shown, (value) => {
    typed.value = value
    unresolved.value = false
  })
  const options = computed(() => offer(typed.value === shown() ? '' : typed.value))
  function pick(text: string): void {
    unresolved.value = !take(text)
  }
  return { typed, unresolved, options, pick }
}
