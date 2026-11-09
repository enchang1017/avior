import { computed, ref, watch } from 'vue'

import { tabsKey } from '@/shared/tabStorage'

/**
 * The console's open statements, up to eight, kept in this browser only and
 * under the name of the operator who wrote them.
 *
 * FEAT-065: a tab is a draft, not a record, so it never reaches the server. It
 * is capped because each one holds an editor state and the DOM budget counts
 * them (ADR 0009 P18). A storage that throws or holds nonsense gives one empty
 * tab rather than an error.
 */
export type EditorTab = { id: string; database: string | null; statement: string }

export const maxTabs = 8
export const tooMany = `Eight statements are open, which is the most. Close one to open another.`

type Stored = { tabs: EditorTab[]; active: string }

function isTab(value: unknown): value is EditorTab {
  if (value === null || typeof value !== 'object') return false
  const tab = value as Record<string, unknown>
  return (
    typeof tab['id'] === 'string' &&
    typeof tab['statement'] === 'string' &&
    (tab['database'] === null || typeof tab['database'] === 'string')
  )
}

let counter = 0
function freshTab(): EditorTab {
  counter += 1
  return { id: `tab-${Date.now().toString(36)}-${counter}`, database: null, statement: '' }
}

export function readTabs(username: string): Stored {
  try {
    const raw = JSON.parse(window.localStorage.getItem(tabsKey(username)) ?? 'null') as unknown
    const stored = raw as Partial<Stored> | null
    const tabs = Array.isArray(stored?.tabs) ? stored.tabs.filter(isTab).slice(0, maxTabs) : []
    if (tabs.length === 0) throw new Error('nothing stored')
    const active = tabs.some((tab) => tab.id === stored?.active)
      ? String(stored?.active)
      : tabs[0]?.id
    return { tabs, active: active ?? '' }
  } catch {
    const tab = freshTab()
    return { tabs: [tab], active: tab.id }
  }
}

function writeTabs(username: string, stored: Stored): void {
  try {
    window.localStorage.setItem(tabsKey(username), JSON.stringify(stored))
  } catch {
    // Not remembered, still open for this session.
  }
}

export function useEditorTabs(username: string) {
  const initial = readTabs(username)
  const tabs = ref<EditorTab[]>(initial.tabs)
  const activeId = ref(initial.active)
  const refusal = ref('')

  const active = computed(
    () => tabs.value.find((tab) => tab.id === activeId.value) ?? (tabs.value[0] as EditorTab),
  )

  watch([tabs, activeId], () => writeTabs(username, { tabs: tabs.value, active: activeId.value }), {
    deep: true,
  })

  function open(): boolean {
    if (tabs.value.length >= maxTabs) {
      refusal.value = tooMany
      return false
    }
    const tab = freshTab()
    tabs.value = [...tabs.value, tab]
    activeId.value = tab.id
    refusal.value = ''
    return true
  }

  function close(id: string): void {
    const at = tabs.value.findIndex((tab) => tab.id === id)
    if (at < 0) return
    const rest = tabs.value.filter((tab) => tab.id !== id)
    tabs.value = rest.length > 0 ? rest : [freshTab()]
    if (activeId.value === id)
      activeId.value = (tabs.value[Math.max(0, at - 1)] ?? tabs.value[0])?.id ?? ''
    refusal.value = ''
  }

  function select(id: string): void {
    if (tabs.value.some((tab) => tab.id === id)) activeId.value = id
  }

  function update(patch: Partial<Omit<EditorTab, 'id'>>): void {
    tabs.value = tabs.value.map((tab) => (tab.id === activeId.value ? { ...tab, ...patch } : tab))
  }

  /**
   * Drops every tab aimed at a database the caller's roles no longer reach.
   *
   * A grant can be withdrawn between two visits, and a restored tab would ask
   * the server for a schema it refuses, as the page's first act.
   */
  function keepReachable(reachable: readonly string[]): void {
    const kept = tabs.value.filter(
      (tab) => tab.database === null || reachable.includes(tab.database),
    )
    if (kept.length === tabs.value.length) return
    tabs.value = kept.length > 0 ? kept : [freshTab()]
    if (!tabs.value.some((tab) => tab.id === activeId.value))
      activeId.value = tabs.value[0]?.id ?? ''
  }

  return { tabs, active, activeId, refusal, open, close, select, update, keepReachable }
}
