import { ref, watch } from 'vue'

/**
 * Whether the rail is folded down to its icons.
 *
 * Folded by default: the 56px instrument rail of FEAT-065, each icon named on
 * hover and on focus. Unfolding shows the labels beside the icons, and the
 * choice is kept in this browser; a storage that throws only means it is not
 * remembered.
 */
const storageKey = 'avior.rail'

export function readCollapsed(): boolean {
  try {
    return window.localStorage.getItem(storageKey) !== 'open'
  } catch {
    return true
  }
}

function write(collapsed: boolean): void {
  try {
    window.localStorage.setItem(storageKey, collapsed ? 'collapsed' : 'open')
  } catch {
    // Not remembered, still applied.
  }
}

const collapsed = ref(readCollapsed())
watch(collapsed, write)

export function useRail() {
  return {
    collapsed,
    toggle: () => {
      collapsed.value = !collapsed.value
    },
    expand: () => {
      collapsed.value = false
    },
  }
}
