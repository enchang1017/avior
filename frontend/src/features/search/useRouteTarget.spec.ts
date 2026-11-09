import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, reactive, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

const route = reactive<{ query: Record<string, unknown> }>({ query: {} })
vi.mock('vue-router', () => ({ useRoute: () => route }))

const { targetFromQuery, useRouteTarget } = await import('./useRouteTarget')

describe('targetFromQuery', () => {
  it('reads a database, and a table when both its parts are there', () => {
    expect(targetFromQuery({})).toBeNull()
    expect(targetFromQuery({ database: ' ' })).toBeNull()
    expect(targetFromQuery({ database: 'aviorfixture' })).toEqual({
      database: 'aviorfixture',
      table: null,
    })
    expect(targetFromQuery({ database: 'aviorfixture', schema: 'public' })).toEqual({
      database: 'aviorfixture',
      table: null,
    })
    expect(
      targetFromQuery({
        database: 'aviorfixture',
        schema: 'public',
        table: 'member_summary',
        kind: 'view',
      }),
    ).toEqual({
      database: 'aviorfixture',
      table: { schema: 'public', name: 'member_summary', kind: 'view' },
    })
    expect(
      targetFromQuery({ database: 'x', schema: 'public', table: 'm', kind: ['odd'] })?.table?.kind,
    ).toBe('table')
  })
})

describe('useRouteTarget', () => {
  it('points the console at what the link names, as picking it in the tree would', async () => {
    const database = ref<string | null>(null)
    const chooseDatabase = vi.fn((name: string | null) => {
      database.value = name
      return Promise.resolve()
    })
    const chooseTable = vi.fn(() => Promise.resolve())
    route.query = { database: 'aviorfixture', schema: 'public', table: 'member' }
    mount(
      defineComponent({
        setup() {
          useRouteTarget({ database, chooseDatabase }, chooseTable)
          return () => h('div')
        },
      }),
    )
    await vi.waitFor(() => expect(chooseTable).toHaveBeenCalled())
    expect(chooseDatabase).toHaveBeenCalledWith('aviorfixture')
    expect(chooseTable).toHaveBeenCalledWith({ schema: 'public', name: 'member', kind: 'table' })

    route.query = { database: 'aviorfixture', schema: 'public', table: 'bet' }
    await nextTick()
    await vi.waitFor(() => expect(chooseTable).toHaveBeenCalledTimes(2))
    expect(chooseDatabase).toHaveBeenCalledTimes(1)

    route.query = {}
    await nextTick()
    expect(chooseTable).toHaveBeenCalledTimes(2)
  })
})
