import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * What the operator is pointed at, for the HUD's TARGET readout.
 *
 * The query console aims it as a database and a table are chosen, and the
 * lineage page at its focus; every other page leaves it where it was, so the readout keeps naming the last thing
 * somebody was about to read rather than going blank on a detour.
 */
export const useTargetStore = defineStore('target', () => {
  const database = ref<string | null>(null)
  const table = ref<string | null>(null)

  function aim(nextDatabase: string | null, nextTable: string | null = null): void {
    database.value = nextDatabase
    table.value = nextDatabase === null ? null : nextTable
  }

  return { database, table, aim }
})
