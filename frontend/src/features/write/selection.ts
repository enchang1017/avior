/**
 * The request j or k moves to, from the one being read.
 *
 * Stops at either end rather than wrapping: somebody working down a queue who
 * lands back on the first request has read it already and cannot tell. A
 * request no longer in the list — decided and gone from the waiting tab —
 * moves to the top or the bottom of what is left.
 */
export function neighbour(ids: readonly string[], current: string, step: 1 | -1): string | null {
  if (ids.length === 0) return null
  const at = ids.indexOf(current)
  if (at === -1) return step === 1 ? (ids[0] ?? null) : (ids.at(-1) ?? null)
  return ids[Math.min(ids.length - 1, Math.max(0, at + step))] ?? null
}

/** Whether a key pressed here was typed into something rather than meant for the page. */
export function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.closest('[contenteditable="true"], [contenteditable=""]') !== null) return true
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)
}
