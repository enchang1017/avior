import { computed, onMounted, ref } from 'vue'

import type { SelectOption } from '@/components'
import type { components } from '@/gen/api'
import { api } from '@/shared/api'
import { useLoad } from './useLoad'

/**
 * The databases a server tab can be pointed at: the observatory's own list,
 * which is already filtered by FEAT-058, so a database the caller does not
 * reach is never offered. The one a drill-down named is chosen when it is
 * offered, the first otherwise, until the operator picks.
 */
export function useDatabasePick(onPicked: (database: string) => void, preferred = '') {
  const list = useLoad<components['schemas']['ObservatoryDatabases']>()
  const database = ref('')

  const options = computed<SelectOption[]>(() =>
    (list.data.value?.databases ?? []).map((row) => ({ value: row.database, label: row.database })),
  )

  function pick(name: string): void {
    database.value = name
    if (name !== '') onPicked(name)
  }

  onMounted(async () => {
    await list.run(() => api.GET('/observatory/databases'))
    const offered = options.value.some((option) => option.value === preferred)
    pick(offered ? preferred : (options.value[0]?.value ?? ''))
  })

  return { list, database, options, pick }
}
