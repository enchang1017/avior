import { describe, expect, it } from 'vitest'

import { useLoad } from './useLoad'

describe('useLoad', () => {
  it('holds the answer and clears the failure', async () => {
    const load = useLoad<{ n: number }>()
    await load.run(() => Promise.resolve({ data: { n: 1 }, response: { status: 200 } }))
    expect(load.data.value).toEqual({ n: 1 })
    expect(load.failure.value).toBeNull()
    expect(load.busy.value).toBe(false)
  })

  it("reads a refusal's message, 409 included", async () => {
    const load = useLoad<{ n: number }>()
    await load.run(() =>
      Promise.resolve({
        error: {
          code: 'observatory.statements_unavailable',
          message: 'pg_stat_statements is not installed in aviordb.',
        },
        response: { status: 409 },
      }),
    )
    expect(load.data.value).toBeNull()
    expect(load.status.value).toBe(409)
    expect(load.failure.value).toBe('pg_stat_statements is not installed in aviordb.')
  })

  it('says the service could not be reached when the request throws', async () => {
    const load = useLoad<{ n: number }>()
    await load.run(() => Promise.reject(new TypeError('Failed to fetch')))
    expect(load.failure.value).toContain('Could not reach the Avior service')
  })

  it('lets only the latest call write', async () => {
    const load = useLoad<{ n: number }>()
    let release: (value: { data: { n: number }; response: { status: number } }) => void = () => {}
    const slow = load.run(() => new Promise((resolve) => (release = resolve)))
    await load.run(() => Promise.resolve({ data: { n: 2 }, response: { status: 200 } }))
    release({ data: { n: 1 }, response: { status: 200 } })
    await slow
    expect(load.data.value).toEqual({ n: 2 })
  })
})
