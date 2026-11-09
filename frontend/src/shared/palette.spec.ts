import { describe, expect, it } from 'vitest'

import {
  arrange,
  search,
  searchEach,
  type PaletteSlot,
} from '@/components/Overlay/VPalette/answers'
import { isPaletteShortcut, registerPaletteSource, usePalette, type PaletteItem } from './palette'

describe('palette', () => {
  it('finds nothing until something registers a source', async () => {
    expect(await search('member')).toEqual([])
  })

  it('asks every registered source and stops asking one that leaves', async () => {
    const item = { id: 't', label: 'public.member', icon: 'table' as const, run: () => undefined }
    const leave = registerPaletteSource((query) => (query === 'mem' ? [item] : []))

    expect(await search('mem')).toEqual([item])
    expect(await search('bet')).toEqual([])

    leave()
    expect(await search('mem')).toEqual([])
  })

  it('opens, closes and toggles from anywhere', () => {
    const first = usePalette()
    const second = usePalette()
    first.show()
    expect(second.open.value).toBe(true)
    second.toggle()
    expect(first.open.value).toBe(false)
    first.toggle()
    first.hide()
    expect(second.open.value).toBe(false)
  })

  it('answers to Cmd+K and Ctrl+K and nothing else', () => {
    const key = (init: KeyboardEventInit) => isPaletteShortcut(new KeyboardEvent('keydown', init))
    expect(key({ key: 'k', metaKey: true })).toBe(true)
    expect(key({ key: 'K', ctrlKey: true })).toBe(true)
    expect(key({ key: 'k' })).toBe(false)
    expect(key({ key: 'k', ctrlKey: true, altKey: true })).toBe(false)
    expect(key({ key: 'j', metaKey: true })).toBe(false)
  })

  it('hands each answer over as it arrives, a quick source before a slow one', async () => {
    const quick = { id: 'q', label: 'Query', icon: 'terminal' as const, run: () => undefined }
    const slow = { id: 's', label: 'public.member', icon: 'table' as const, run: () => undefined }
    let release: (value: PaletteItem[]) => void = () => undefined
    const leaveSlow = registerPaletteSource(
      () => new Promise<PaletteItem[]>((resolve) => (release = resolve)),
    )
    const leaveQuick = registerPaletteSource(() => ({ items: [quick], status: 'from memory' }))
    const seen: [number, PaletteSlot][] = []
    const done = searchEach('q', new AbortController().signal, (index, slot) =>
      seen.push([index, slot]),
    )
    await Promise.resolve()
    await Promise.resolve()
    expect(seen).toEqual([[1, { items: [quick], status: 'from memory' }]])
    release([slow])
    await done
    expect(seen[1]).toEqual([0, { items: [slow], status: '' }])
    leaveSlow()
    leaveQuick()
  })

  it('hands nothing over once aborted, and passes the signal to every source', async () => {
    const signals: AbortSignal[] = []
    const leave = registerPaletteSource((_, signal) => {
      signals.push(signal)
      return new Promise<PaletteItem[]>((resolve, reject) => {
        signal.addEventListener('abort', () => reject(new DOMException('gone', 'AbortError')))
        setTimeout(() => resolve([]), 50)
      })
    })
    const controller = new AbortController()
    const seen: number[] = []
    const done = searchEach('mem', controller.signal, (index) => seen.push(index))
    controller.abort()
    await done
    expect(signals[0]?.aborted).toBe(true)
    expect(seen).toEqual([])
    leave()
  })

  it('turns a failing source into a status line', async () => {
    const leave = registerPaletteSource(() => Promise.reject(new Error('Search is down.')))
    const leaveOdd = registerPaletteSource(() => {
      throw new Error('Tables are unavailable.')
    })
    const seen: PaletteSlot[] = []
    await searchEach('mem', new AbortController().signal, (index, slot) => (seen[index] = slot))
    expect(seen).toEqual([
      { items: [], status: 'Search is down.' },
      { items: [], status: 'Tables are unavailable.' },
    ])
    leave()
    leaveOdd()
  })

  it('groups answers under their headings in the order they arrived', () => {
    const item = (id: string, group?: string): PaletteItem => ({
      id,
      label: id,
      icon: 'table',
      run: () => undefined,
      ...(group === undefined ? {} : { group }),
    })
    const slots: (PaletteSlot | undefined)[] = [
      {
        items: [item('t1', 'Tables'), item('d1', 'Databases'), item('t2', 'Tables')],
        status: 'still indexing 1 database',
      },
      { items: [item('p1', 'Pages'), item('loose')], status: '' },
      undefined,
    ]
    const { sections, flat, statuses } = arrange(slots, [1, 0])
    expect(sections.map((s) => s.group)).toEqual(['Pages', '', 'Tables', 'Databases'])
    expect(flat.map((i) => i.id)).toEqual(['p1', 'loose', 't1', 't2', 'd1'])
    expect(sections[2]?.items.map((e) => e.index)).toEqual([2, 3])
    expect(statuses).toEqual(['still indexing 1 database'])
    expect(arrange(slots, [2])).toEqual({ sections: [], flat: [], statuses: [] })
  })
})
