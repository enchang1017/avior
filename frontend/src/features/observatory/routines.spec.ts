import { describe, expect, it } from 'vitest'

import {
  meanText,
  parseRoutineState,
  routineBars,
  routineDrill,
  anySlowest,
  ariaSort,
  nextSort,
  hiddenText,
  visibleRoutines,
  definitionLink,
  routineLabel,
  routineName,
  routineQuery,
  routineRows,
  routineStateQuery,
  timeText,
  trackedSince,
  versionCountText,
  type Routine,
  type RoutineTimingGroup,
} from './routines'

function routine(overrides: Partial<Routine>): Routine {
  return {
    database: 'aviorfixture',
    schema: 'public',
    name: 'settle',
    signature: 'bigint',
    kind: 'function',
    timed: true,
    notTimedReason: null,
    calls: 10,
    totalMs: 100,
    selfMs: 60,
    meanMs: 10,
    maxCallMs: null,
    ...overrides,
  }
}

const group: RoutineTimingGroup = {
  database: 'aviorfixture',
  schema: 'public',
  base: 'shop_settle',
  latest: 'shop_settle_26_09_25',
  versions: [
    { name: 'shop_settle_26_08_01', signature: 'bigint', date: '2026-08-01' },
    { name: 'shop_settle_26_09_25', signature: 'bigint', date: '2026-09-25' },
    { name: 'shop_settle_26_09_01', signature: 'bigint', date: '2026-09-01' },
  ],
}

const version = (name: string, totalMs: number) => routine({ name, totalMs })

describe('the routine timing address', () => {
  it('defaults to seven days sorted by mean per call, with no filter', () => {
    expect(parseRoutineState({})).toEqual({
      range: '7d',
      sort: 'mean',
      order: 'desc',
      database: '',
      search: '',
      hideUncalled: true,
    })
  })

  it('reads every choice back and ignores values it does not know', () => {
    expect(
      parseRoutineState({
        range: '30d',
        sort: 'total',
        order: 'desc',
        database: 'ledger',
        search: ' settle ',
      }),
    ).toEqual({
      range: '30d',
      sort: 'total',
      order: 'desc',
      database: 'ledger',
      search: 'settle',
      hideUncalled: true,
    })
    expect(parseRoutineState({ uncalled: 'shown' }).hideUncalled).toBe(false)
    expect(parseRoutineState({ range: '1y', sort: 'slow', database: ['a'] })).toEqual({
      range: '7d',
      sort: 'mean',
      order: 'desc',
      database: '',
      search: '',
      hideUncalled: true,
    })
  })

  it('writes the window and sort always, and a filter only when one is set', () => {
    expect(
      routineStateQuery({
        range: '24h',
        sort: 'calls',
        order: 'desc',
        database: '',
        search: '',
        hideUncalled: true,
      }),
    ).toEqual({ range: '24h', sort: 'calls' })
    const full = {
      range: '7d',
      sort: 'total',
      order: 'desc',
      database: 'ledger',
      search: 'public.x',
      hideUncalled: false,
    } as const
    expect(routineStateQuery(full)).toMatchObject({ uncalled: 'shown' })
    expect(parseRoutineState(routineStateQuery(full))).toEqual(full)
  })

  it('asks the server for the same choices, with a limit when one is given', () => {
    expect(
      routineQuery({
        range: '7d',
        sort: 'mean',
        order: 'desc',
        database: '',
        search: '',
        hideUncalled: true,
      }),
    ).toEqual({
      range: '7d',
      sort: 'mean',
    })
    expect(
      routineQuery(
        {
          range: '30d',
          sort: 'total',
          order: 'desc',
          database: 'ledger',
          search: 'settle',
          hideUncalled: false,
        },
        10,
      ),
    ).toEqual({
      range: '30d',
      sort: 'total',
      database: 'ledger',
      search: 'settle',
      limit: 10,
    })
  })
})

describe('the routine timing rows', () => {
  it('draws a date-versioned routine inside its group, newest first, the latest marked', () => {
    const routines = [
      routine({ name: 'lookup', totalMs: 900 }),
      version('shop_settle_26_09_01', 500),
      routine({ name: 'settle', totalMs: 400 }),
      version('shop_settle_26_09_25', 300),
      version('shop_settle_26_08_01', 200),
    ]
    const rows = routineRows(routines, [group])
    expect(
      rows.map((row) =>
        row.row === 'group'
          ? `group ${row.group.base} ${row.count}`
          : `${row.row} ${row.routine.name}${row.row === 'version' && row.latest ? ' latest' : ''}`,
      ),
    ).toEqual([
      'routine lookup',
      'group shop_settle 3',
      'version shop_settle_26_09_25 latest',
      'version shop_settle_26_09_01',
      'version shop_settle_26_08_01',
      'routine settle',
    ])
    expect(new Set(rows.map((row) => row.key)).size).toBe(rows.length)
  })

  it('counts only the versions answered, and drops a group none of whose versions was', () => {
    const two = routineRows(
      [version('shop_settle_26_08_01', 1), version('shop_settle_26_09_25', 2)],
      [group],
    )
    expect(two.map((row) => row.row)).toEqual(['group', 'version', 'version'])
    expect(two[0]).toMatchObject({ count: 2 })
    expect(routineRows([routine({ name: 'other' })], [group]).map((row) => row.row)).toEqual([
      'routine',
    ])
  })

  it('draws a group left with one version as that version plain, with no header', () => {
    const rows = routineRows([version('shop_settle_26_08_01', 1)], [group])
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ row: 'routine' })
  })

  it('keeps a group to its own database and overloads apart by signature', () => {
    const elsewhere = routine({ database: 'ledger', name: 'shop_settle_26_09_25' })
    const overload = routine({ name: 'shop_settle_26_09_25', signature: 'text' })
    expect(routineRows([elsewhere, overload], [group]).map((row) => row.row)).toEqual([
      'routine',
      'routine',
    ])
  })

  it('writes a routine with its arguments', () => {
    expect(routineName(routine({}))).toBe('public.settle(bigint)')
    expect(routineName(routine({ signature: '' }))).toBe('public.settle()')
    expect(versionCountText(1)).toBe('1 version')
    expect(versionCountText(3)).toBe('3 versions')
  })
})

describe('the routine timing figures', () => {
  it('shows a dash for the mean of a routine not called, and its zero time', () => {
    const uncalled = routine({ calls: 0, totalMs: 0, selfMs: 0, meanMs: null })
    expect(meanText(uncalled)).toBe('—')
    expect(timeText(uncalled, uncalled.totalMs)).toBe('0 ms')
    expect(meanText(routine({ meanMs: 1500 }))).toBe('1.5 s')
  })

  it('shows why a routine is not timed, and no time for it', () => {
    const sql = routine({
      timed: false,
      notTimedReason: 'LANGUAGE sql needs track_functions = all',
      meanMs: null,
    })
    expect(meanText(sql)).toBe('not timed (LANGUAGE sql needs track_functions = all)')
    expect(meanText({ ...sql, notTimedReason: 'not timed (already said)' })).toBe(
      'not timed (already said)',
    )
    expect(timeText(sql, 0)).toBe('—')
  })

  it("states the chosen database's tracking date, or the earliest of all", () => {
    const tracking = [
      { database: 'aviorfixture', since: '2026-09-27T00:00:00Z' },
      { database: 'ledger', since: '2026-09-01T00:00:00Z' },
      { database: 'silent', since: null },
    ]
    expect(trackedSince(tracking, '')).toBe('2026-09-01T00:00:00Z')
    expect(trackedSince(tracking, 'aviorfixture')).toBe('2026-09-27T00:00:00Z')
    expect(trackedSince(tracking, 'silent')).toBeNull()
    expect(trackedSince([], '')).toBeNull()
  })
})

describe('the overview routine chart', () => {
  const answered = [
    routine({ name: 'settle', totalMs: 9_000, calls: 1_200, meanMs: 7.5 }),
    routine({ database: 'ledger', schema: 'ops', name: 'close_day', totalMs: 4_000, calls: 2 }),
  ]

  it('draws one bar per routine by total time, with its calls and mean', () => {
    expect(routineBars(answered)).toEqual([
      {
        id: 'aviorfixture\u0000public\u0000settle\u0000bigint',
        label: 'public.settle',
        value: 9_000,
        detail: '1,200 calls · 7.5 ms mean',
      },
      {
        id: 'ledger\u0000ops\u0000close_day\u0000bigint',
        label: 'ops.close_day',
        value: 4_000,
        detail: '2 calls · 10 ms mean',
      },
    ])
    expect(routineBars(answered, 1)).toHaveLength(1)
  })

  it('opens the tab sorted by total time over the range, and a bar searched to its routine', () => {
    expect(routineDrill('30d', '')).toEqual({
      name: 'observatory',
      query: { tab: 'routines', sort: 'total', range: '30d' },
    })
    expect(routineDrill('24h', 'aviorfixture')).toEqual({
      name: 'observatory',
      query: { tab: 'routines', sort: 'total', range: '24h', database: 'aviorfixture' },
    })
    const bar = routineBars(answered)[1]?.id ?? ''
    expect(routineDrill('7d', '', bar)).toEqual({
      name: 'observatory',
      query: {
        tab: 'routines',
        sort: 'total',
        range: '7d',
        database: 'ledger',
        search: 'ops.close_day',
      },
    })
  })
})

describe('a routine as a link', () => {
  it('shows the name alone, its date version included, and keeps the signature for the title', () => {
    const versioned = routine({ name: 'settle_bonus_26_09_10', signature: 'bigint' })
    expect(routineLabel(versioned)).toBe('public.settle_bonus_26_09_10')
    expect(routineName(versioned)).toBe('public.settle_bonus_26_09_10(bigint)')
  })

  it('opens a function in the function viewer and a procedure in the procedure viewer', () => {
    expect(definitionLink(routine({ database: 'aviorlineage' }))).toEqual({
      name: 'objects-function',
      query: { database: 'aviorlineage', schema: 'public', name: 'settle' },
    })
    expect(definitionLink(routine({ kind: 'procedure' }))).toMatchObject({
      name: 'objects-stored-procedure',
    })
  })
})

describe('the slowest-call column', () => {
  it('is shown only when a routine on the page has a slowest call', () => {
    expect(anySlowest([routine({ maxCallMs: null }), routine({ maxCallMs: null })])).toBe(false)
    expect(anySlowest([routine({ maxCallMs: null }), routine({ maxCallMs: 12 })])).toBe(true)
    expect(anySlowest([])).toBe(false)
  })
})

describe('routines with no calls', () => {
  const called = routine({ name: 'called', calls: 3 })
  const idle = [routine({ name: 'a', calls: 0 }), routine({ name: 'b', calls: 0 })]

  it('are hidden by default and counted', () => {
    expect(visibleRoutines([called, ...idle], true)).toEqual({ shown: [called], hidden: 2 })
    expect(hiddenText(2)).toBe('2 routines with no calls hidden')
    expect(hiddenText(1)).toBe('1 routine with no calls hidden')
  })

  it('are all shown when the operator asks', () => {
    expect(visibleRoutines([called, ...idle], false)).toEqual({
      shown: [called, ...idle],
      hidden: 0,
    })
  })

  it('take a group header with them when no version was called', () => {
    const versions = [
      routine({ name: 'shop_settle_26_08_01', calls: 0 }),
      routine({ name: 'shop_settle_26_09_25', calls: 0 }),
    ]
    const { shown } = visibleRoutines([called, ...versions], true)
    expect(routineRows(shown, [group]).map((row) => row.row)).toEqual(['routine'])
  })
})

describe('sorting by a header', () => {
  it('opens a new column largest first and turns the sorted one round', () => {
    expect(nextSort({ sort: 'mean', order: 'desc' }, 'total')).toEqual({
      sort: 'total',
      order: 'desc',
    })
    expect(nextSort({ sort: 'total', order: 'desc' }, 'total')).toEqual({
      sort: 'total',
      order: 'asc',
    })
    expect(nextSort({ sort: 'total', order: 'asc' }, 'total')).toEqual({
      sort: 'total',
      order: 'desc',
    })
    expect(nextSort({ sort: 'total', order: 'asc' }, 'slowest')).toEqual({
      sort: 'slowest',
      order: 'desc',
    })
  })

  it('tells assistive technology which column is sorted and which way', () => {
    expect(ariaSort({ sort: 'calls', order: 'desc' }, 'calls')).toBe('descending')
    expect(ariaSort({ sort: 'calls', order: 'asc' }, 'calls')).toBe('ascending')
    expect(ariaSort({ sort: 'calls', order: 'asc' }, 'mean')).toBe('none')
  })

  it('keeps the direction in the address only when it is ascending', () => {
    const state = {
      range: '7d',
      sort: 'slowest',
      order: 'asc',
      database: '',
      search: '',
      hideUncalled: true,
    } as const
    expect(routineStateQuery(state)).toEqual({ range: '7d', sort: 'slowest', order: 'asc' })
    expect(parseRoutineState(routineStateQuery(state))).toEqual(state)
    expect(parseRoutineState({ order: 'sideways' }).order).toBe('desc')
    expect(routineQuery(state)).toEqual({ range: '7d', sort: 'slowest', order: 'asc' })
  })
})
