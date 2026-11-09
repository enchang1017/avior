import { usePalette, type PaletteAnswer, type PaletteItem } from '@/shared/palette'

export type PaletteSlot = { items: PaletteItem[]; status: string }

function slotOf(answer: PaletteAnswer): PaletteSlot {
  return Array.isArray(answer)
    ? { items: answer, status: '' }
    : { items: answer.items, status: answer.status ?? '' }
}

type Asked = { slot: PaletteSlot; again?: () => Promise<PaletteAnswer> }

async function settle(
  answer: () => PaletteAnswer | Promise<PaletteAnswer>,
  signal: AbortSignal,
): Promise<Asked | null> {
  try {
    const given = await answer()
    return { slot: slotOf(given), ...(Array.isArray(given) ? {} : { again: given.again }) }
  } catch (thrown) {
    if (signal.aborted) return null
    return {
      slot: { items: [], status: thrown instanceof Error ? thrown.message : 'A search failed.' },
    }
  }
}

/**
 * Asks every source and hands each answer over the moment it arrives, so a
 * quick source is never held back by a slow one. A source that says its answer
 * is incomplete is followed, and each later answer replaces its earlier one
 * under the same index. Nothing is handed over once the signal is aborted, and
 * a source that fails says so in its status.
 */
export function searchEach(
  query: string,
  signal: AbortSignal,
  answered: (index: number, slot: PaletteSlot) => void,
): Promise<void> {
  const asked = usePalette().sources.value.map(async (source, index) => {
    let next = await settle(() => source(query, signal), signal)
    while (next !== null && !signal.aborted) {
      answered(index, next.slot)
      const again = next.again
      if (again === undefined) return
      next = await settle(again, signal)
    }
  })
  return Promise.all(asked).then(() => undefined)
}

export async function search(
  query: string,
  signal: AbortSignal = new AbortController().signal,
): Promise<PaletteItem[]> {
  const slots: PaletteSlot[] = []
  await searchEach(query, signal, (index, slot) => {
    slots[index] = slot
  })
  return slots.flatMap((slot) => slot.items)
}

export type PaletteSection = { group: string; items: { item: PaletteItem; index: number }[] }

/**
 * The answers so far, in the order they arrived, grouped under their headings
 * in the order each heading first appears. `flat` is the keyboard's order, and
 * each section item carries its place in it.
 */
export function arrange(
  slots: readonly (PaletteSlot | undefined)[],
  arrival: readonly number[],
): { sections: PaletteSection[]; flat: PaletteItem[]; statuses: string[] } {
  const answered = arrival.flatMap((index) => {
    const slot = slots[index]
    return slot === undefined ? [] : [slot]
  })
  const groups = new Map<string, PaletteItem[]>()
  for (const item of answered.flatMap((slot) => slot.items)) {
    const group = item.group ?? ''
    groups.set(group, [...(groups.get(group) ?? []), item])
  }
  const flat: PaletteItem[] = []
  const sections = [...groups].map(([group, items]) => ({
    group,
    items: items.map((item) => ({ item, index: flat.push(item) - 1 })),
  }))
  const statuses = answered.map((slot) => slot.status).filter((status) => status !== '')
  return { sections, flat, statuses }
}
