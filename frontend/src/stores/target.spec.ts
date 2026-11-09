import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { useTargetStore } from './target'

describe('target', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('starts pointed at nothing', () => {
    const target = useTargetStore()
    expect([target.database, target.table]).toEqual([null, null])
  })

  it('aims at a database and a table', () => {
    const target = useTargetStore()
    target.aim('aviorfixture', 'public.member')
    expect([target.database, target.table]).toEqual(['aviorfixture', 'public.member'])
  })

  // A table without its database names nothing an operator could run against.
  it('drops the table when the database goes', () => {
    const target = useTargetStore()
    target.aim('aviorfixture', 'public.member')
    target.aim(null, 'public.member')
    expect([target.database, target.table]).toEqual([null, null])
  })
})
