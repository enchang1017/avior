import { nextTick, ref } from 'vue'

/**
 * The parts of a dropdown that have nothing to do with what is in it.
 *
 * Opening, closing, moving a highlight with the keyboard, and not letting the
 * blur that a click causes cancel the click. Two controls need all of that and
 * disagree only about whether a value outside the list is allowed, so it lives
 * here rather than twice.
 */
export function useListbox(count: () => number, choose: (index: number) => void) {
  const open = ref(false)
  const active = ref(-1)

  function show() {
    open.value = true
  }

  function close() {
    open.value = false
    active.value = -1
  }

  function toggle() {
    if (open.value) close()
    else show()
  }

  function move(by: number) {
    if (!open.value) {
      show()
      return
    }
    const size = count()
    if (size === 0) return
    // From nothing, down takes the first and up takes the last. Treating the
    // absence as index -1 and adding would send up to the second from the top,
    // which is neither end and reads as the control ignoring the key.
    if (active.value < 0) {
      active.value = by > 0 ? 0 : size - 1
      return
    }
    active.value = (active.value + by + size) % size
  }

  /**
   * Enter belongs to the form unless an option is highlighted.
   *
   * Somebody who typed a table name and pressed enter meant "apply the filter".
   * Taking that keystroke because a list happened to be open would make the
   * control feel like it was fighting them.
   */
  function confirm(event: KeyboardEvent) {
    if (!open.value || active.value < 0) return
    event.preventDefault()
    choose(active.value)
  }

  /**
   * Closing is deferred by a tick so that choosing an option is not cancelled
   * by the blur that clicking it causes.
   */
  function blur() {
    void nextTick(close)
  }

  return { open, active, show, close, toggle, move, confirm, blur }
}
