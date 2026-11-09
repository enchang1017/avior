/**
 * Gives the tests back the storage Node took away.
 *
 * Node 22 added a `localStorage` global of its own, and it is undefined unless
 * the process was started with --localstorage-file. It shadows the one jsdom
 * provides, so `window.localStorage` is undefined under vitest and every test
 * that touches it fails on a runtime upgrade rather than on a change anybody
 * made — which is exactly how it arrived: four assertions in theme.spec went
 * red with nothing in their history to explain it.
 *
 * A Map behind the Storage interface rather than a reach for jsdom's internals:
 * what the tests need is somewhere to put a string and get it back, and the
 * real thing's quota and events are not what any of them assert.
 */
function inMemoryStorage(): Storage {
  const held = new Map<string, string>()
  return {
    get length() {
      return held.size
    },
    key: (index: number) => [...held.keys()][index] ?? null,
    getItem: (key: string) => held.get(key) ?? null,
    setItem: (key: string, value: string) => void held.set(key, String(value)),
    removeItem: (key: string) => void held.delete(key),
    clear: () => held.clear(),
  }
}

for (const name of ['localStorage', 'sessionStorage'] as const) {
  if (window[name] === undefined) {
    Object.defineProperty(window, name, { value: inMemoryStorage(), configurable: true })
  }
}
