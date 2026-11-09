import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import { readCollapsed, useRail } from './railState'

describe('the rail', () => {
  it('is folded until somebody opens it, and remembers', async () => {
    window.localStorage.clear()
    expect(readCollapsed()).toBe(true)

    const rail = useRail()
    rail.expand()
    await nextTick()
    expect(readCollapsed()).toBe(false)

    rail.toggle()
    await nextTick()
    expect(rail.collapsed.value).toBe(true)
    expect(readCollapsed()).toBe(true)
  })

  it('is folded when storage cannot be read', () => {
    const denied = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(readCollapsed()).toBe(true)
    denied.mockRestore()
  })
})
