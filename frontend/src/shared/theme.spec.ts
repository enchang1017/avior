import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apply, read, themes, useTheme } from './theme'

function root(): HTMLElement {
  return document.documentElement
}

describe('read', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('defaults to night', () => {
    expect(read()).toBe('dark')
  })

  it('returns light once it was chosen', () => {
    window.localStorage.setItem('avior.theme', 'light')
    expect(read()).toBe('light')
  })

  // An earlier build stored `system`; the OS no longer decides, so it is night.
  it('reads anything but light as night, the old system choice included', () => {
    window.localStorage.setItem('avior.theme', 'system')
    expect(read()).toBe('dark')
    window.localStorage.setItem('avior.theme', 'solarized')
    expect(read()).toBe('dark')
  })

  it('survives storage being unavailable', () => {
    const denied = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('access denied')
    })

    expect(read()).toBe('dark')

    denied.mockRestore()
  })
})

describe('apply', () => {
  afterEach(() => {
    root().removeAttribute('data-theme')
    root().style.colorScheme = ''
  })

  it('stamps the choice on the root', () => {
    apply('light')
    expect(root().getAttribute('data-theme')).toBe('light')
    expect(root().style.colorScheme).toBe('light')

    apply('dark')
    expect(root().getAttribute('data-theme')).toBe('dark')
  })
})

describe('useTheme', () => {
  afterEach(() => {
    root().removeAttribute('data-theme')
  })

  it('toggles between night and day and back', () => {
    const { theme, toggle } = useTheme()
    const first = theme.value
    toggle()
    expect(theme.value).not.toBe(first)
    toggle()
    expect(theme.value).toBe(first)
    expect(themes).toEqual(['dark', 'light'])
  })

  it('shares one state across callers', () => {
    const first = useTheme()
    const second = useTheme()

    first.set('light')

    expect(second.theme.value).toBe('light')
  })
})
