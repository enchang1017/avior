import { ref, watch } from 'vue'

/**
 * Two states: night, which is the default, and the day chart.
 *
 * The console is dark unless the operator chooses light, whatever the operating
 * system prefers (ADR 0009, Dark-first). Following the system was considered
 * and rejected: most office laptops here default to light, and "dark-first"
 * would then mean dark only for people who had already chosen dark.
 */
export const themes = ['dark', 'light'] as const
export type Theme = (typeof themes)[number]

const storageKey = 'avior.theme'

/**
 * read returns the stored choice, or `dark`.
 *
 * Anything but `light` is night — including a `system` an earlier build
 * stored. Every access is guarded: a private window, cleared site data, or a
 * browser set to block storage all make this throw rather than return null,
 * and a theme preference is not worth a blank page.
 */
export function read(): Theme {
  try {
    return window.localStorage.getItem(storageKey) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

function write(theme: Theme): void {
  try {
    window.localStorage.setItem(storageKey, theme)
  } catch {
    // A preference that cannot be remembered still applies for this session.
  }
}

/**
 * apply stamps the choice on the root element.
 *
 * The stylesheet is night until `data-theme="light"` says otherwise, so a page
 * the script has not reached yet is night, which is the default, rather than a
 * flash of day. `color-scheme` follows, so scrollbars and form controls do too.
 */
export function apply(theme: Theme): void {
  const root = document.documentElement
  root.setAttribute('data-theme', theme)
  root.style.colorScheme = theme
}

const current = ref<Theme>('dark')
let started = false

/**
 * useTheme exposes the preference. The state is module-level so a toggle in the
 * top bar and anything else reading the theme cannot disagree.
 */
export function useTheme() {
  if (!started) {
    started = true
    current.value = read()
    apply(current.value)
    watch(current, (theme) => {
      apply(theme)
      write(theme)
    })
  }

  function set(theme: Theme): void {
    current.value = theme
  }

  /** toggle flips night and day, which is what one button can do. */
  function toggle(): void {
    current.value = current.value === 'dark' ? 'light' : 'dark'
  }

  return { theme: current, set, toggle }
}
