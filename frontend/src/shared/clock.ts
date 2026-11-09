import { onBeforeUnmount, onMounted, ref } from 'vue'

/** HH:MM in UTC: the time the audit log and every server record are kept in. */
export function utcMinute(at: Date): string {
  return at.toISOString().slice(11, 16)
}

/**
 * The time, updated once a minute on the minute.
 *
 * Not every second: a ticking seconds field is a repaint a second for as long as
 * the tab is open, to show a number nobody reads.
 */
export function useUtcClock() {
  const now = ref(utcMinute(new Date()))
  let timer: ReturnType<typeof setTimeout> | undefined

  function schedule(): void {
    const at = new Date()
    now.value = utcMinute(at)
    const untilNextMinute = 60_000 - (at.getTime() % 60_000)
    timer = setTimeout(schedule, untilNextMinute)
  }

  onMounted(schedule)
  onBeforeUnmount(() => clearTimeout(timer))

  return now
}
