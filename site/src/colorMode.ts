import { useCallback, useEffect, useState } from 'react'

export type ColorMode = 'light' | 'dark' | 'system'
export type ResolvedMode = Exclude<ColorMode, 'system'>

const STORAGE_KEY = 'avior-color-mode'
const DARK_QUERY = '(prefers-color-scheme: dark)'

function isColorMode(value: unknown): value is ColorMode {
  return value === 'light' || value === 'dark' || value === 'system'
}

function readStored(): ColorMode {
  try {
    const value = localStorage.getItem(STORAGE_KEY)
    return isColorMode(value) ? value : 'system'
  } catch {
    return 'system'
  }
}

function writeStored(mode: ColorMode) {
  try {
    localStorage.setItem(STORAGE_KEY, mode)
  } catch {
    // Storage can be unavailable in a private window; the choice then lasts for this visit only.
  }
}

function systemMode(): ResolvedMode {
  return matchMedia(DARK_QUERY).matches ? 'dark' : 'light'
}

export function useColorMode() {
  const [mode, setModeState] = useState<ColorMode>(readStored)
  const [system, setSystem] = useState<ResolvedMode>(systemMode)

  useEffect(() => {
    const query = matchMedia(DARK_QUERY)
    const onChange = () => setSystem(query.matches ? 'dark' : 'light')
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  const resolved: ResolvedMode = mode === 'system' ? system : mode

  useEffect(() => {
    document.documentElement.dataset.mode = resolved
  }, [resolved])

  const setMode = useCallback((next: ColorMode) => {
    setModeState(next)
    writeStored(next)
  }, [])

  return { mode, resolved, setMode }
}

export { isColorMode }
