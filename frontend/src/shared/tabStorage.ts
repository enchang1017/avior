/**
 * Where the console keeps an operator's open statements in this browser.
 *
 * One key per username: a statement is a draft written by one person against
 * databases their roles reach, and a browser is shared by whoever signs in on
 * it next. The unkeyed name is the store before it was split, kept only so it
 * is cleared with the rest.
 */
const prefix = 'avior.tabs'

export function tabsKey(username: string): string {
  return `${prefix}.${username}`
}

export function forgetAllTabs(): void {
  try {
    const stored = window.localStorage
    const keys = Array.from({ length: stored.length }, (_, at) => stored.key(at) ?? '')
    for (const key of keys) {
      if (key === prefix || key.startsWith(`${prefix}.`)) stored.removeItem(key)
    }
  } catch {
    // Storage that cannot be read holds nothing this browser can show again.
  }
}
