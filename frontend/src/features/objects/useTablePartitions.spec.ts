import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { countStatement, quoted, useTablePartitions } = await import('./useTablePartitions')

const instanceId = '00000000-0000-0000-0000-0000000000a1'

const september = {
  schema: 'public',
  name: 'rounds_2026_09',
  bound: "FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')",
  isDefault: false,
  partitioned: false,
  approxRows: 1200,
  totalBytes: 65536,
}

const partitioning = {
  schema: 'public',
  name: 'rounds',
  strategy: 'range' as const,
  key: 'RANGE (id)',
  approxRows: 1200,
  totalBytes: 65536,
  partitions: [september],
  warnings: [],
}

const limits = { maxCost: 50, maxRows: 1000 }

describe('useTablePartitions', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('reads the partitioning of the table it is asked about', async () => {
    get.mockResolvedValue({ data: partitioning, response: { status: 200 } })
    const partitions = useTablePartitions(instanceId)

    await partitions.load('aviorfixture', 'public', 'rounds')

    expect(get).toHaveBeenCalledWith('/schema/partitions', {
      params: {
        query: { instanceId, database: 'aviorfixture', schema: 'public', table: 'rounds' },
      },
    })
    expect(partitions.partitioning.value).toEqual(partitioning)
    expect(partitions.failure.value).toBeNull()
  })

  it('reports a refusal rather than an empty table', async () => {
    get.mockResolvedValue({
      data: undefined,
      error: {
        code: 'access.database_not_granted',
        message: 'Your roles do not reach aviorfixture.',
      },
      response: { status: 403 },
    })
    const partitions = useTablePartitions(instanceId)

    await partitions.load('aviorfixture', 'public', 'rounds')

    expect(partitions.partitioning.value).toBeNull()
    expect(partitions.failure.value).toEqual({
      kind: 'error',
      code: 'access.database_not_granted',
      message: 'Your roles do not reach aviorfixture.',
    })
  })

  // The exact count is an ordinary statement through the guard: audited, held
  // to the cost cap, and run only when asked for.
  it('counts one partition exactly through the guarded path', async () => {
    post.mockResolvedValue({ data: { rows: [['1234567890123456789']] }, response: { status: 200 } })
    const partitions = useTablePartitions(instanceId)

    await partitions.countExactly('aviorfixture', september)

    expect(post).toHaveBeenCalledWith('/query/execute', {
      body: {
        instanceId,
        database: 'aviorfixture',
        statement: 'SELECT count(ctid) AS exact_rows FROM "public"."rounds_2026_09"',
      },
    })
    expect(partitions.counts.value['public.rounds_2026_09']).toEqual({
      state: 'counted',
      rows: '1234567890123456789',
    })
  })

  it('says why a count was refused', async () => {
    post.mockResolvedValue({
      data: undefined,
      error: {
        decision: 'deny',
        reasons: [
          { rule: 'cost_limit_exceeded', message: 'estimated plan cost 90 exceeds the limit' },
        ],
        referencedTables: [],
        referencedColumns: [],
        limits,
      },
      response: { status: 403 },
    })
    const partitions = useTablePartitions(instanceId)

    await partitions.countExactly('aviorfixture', september)

    expect(partitions.counts.value['public.rounds_2026_09']).toEqual({
      state: 'refused',
      message: 'estimated plan cost 90 exceeds the limit',
    })
  })

  it('forgets the last table when asked to', async () => {
    get.mockResolvedValue({ data: partitioning, response: { status: 200 } })
    const partitions = useTablePartitions(instanceId)
    await partitions.load('aviorfixture', 'public', 'rounds')

    partitions.clear()

    expect(partitions.partitioning.value).toBeNull()
    expect(partitions.counts.value).toEqual({})
  })

  it('quotes a name exactly, case and quotes included', () => {
    expect(quoted('Rounds"x')).toBe('"Rounds""x"')
    expect(countStatement({ schema: 'Audit', name: 'outbox_default' })).toBe(
      'SELECT count(ctid) AS exact_rows FROM "Audit"."outbox_default"',
    )
  })
})
