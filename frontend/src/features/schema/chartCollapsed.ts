import { ref, watch } from 'vue'

export const collapsedKey = 'avior.schema-chart.collapsed'

function remembered(): boolean {
  try {
    return window.localStorage.getItem(collapsedKey) === 'true'
  } catch {
    return false
  }
}

function remember(collapsed: boolean): void {
  try {
    window.localStorage.setItem(collapsedKey, String(collapsed))
  } catch {
    return
  }
}

/** Whether the chart is folded away, remembered per browser. */
export function useChartCollapsed() {
  const collapsed = ref(remembered())
  watch(collapsed, remember)
  return collapsed
}
