import { nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type * as sharedApi from '@/shared/api'

const get = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get } }
})

const { useObjectBrowser, objectTypeLabels } = await import('./useObjectBrowser')

const instanceId = '00000000-0000-0000-0000-0000000000a1'

const memberBets = { schema: 'public', name: 'sp_member_bets' }
const settleBet = { schema: 'public', name: 'sp_settle_bet' }
const objects = [memberBets, settleBet]

const definition = {
  type: 'stored_procedure' as const,
  schema: 'public',
  name: 'sp_member_bets',
  definition: 'CREATE OR REPLACE PROCEDURE public.sp_member_bets(IN p_member_id integer)',
  grants: ['GRANT EXECUTE ON PROCEDURE public.sp_member_bets(integer) TO avior_reader;'],
}

function ok<T>(data: T) {
  return { data, response: { status: 200 } }
}

function routeGet(answers: Record<string, unknown>) {
  get.mockImplementation((path: string) => {
    if (path in answers) return Promise.resolve(ok(answers[path]))
    return Promise.resolve({ data: undefined, error: null, response: { status: 404 } })
  })
}

async function browseToDefinition() {
  routeGet({
    '/schema/databases': ['aviorfixture'],
    '/objects': objects,
    '/objects/definition': definition,
  })
  const browser = useObjectBrowser(instanceId, () => 'stored_procedure')
  await browser.loadDatabases()
  await browser.chooseDatabase('aviorfixture')
  await browser.choose(memberBets)
  return browser
}

describe('useObjectBrowser', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('asks for the type it was built for', async () => {
    routeGet({ '/schema/databases': ['aviorfixture'], '/objects': objects })
    const browser = useObjectBrowser(instanceId, () => 'view')

    await browser.chooseDatabase('aviorfixture')

    expect(get).toHaveBeenCalledWith('/objects', {
      params: { query: { instanceId, database: 'aviorfixture', type: 'view' } },
    })
  })

  it('walks database to object to definition', async () => {
    const browser = await browseToDefinition()

    expect(browser.objects.value).toEqual(objects)
    expect(browser.definition.value).toEqual(definition)
  })

  // A definition left on screen after the database changed reads as belonging
  // to the new one.
  it('clears the definition when the database changes', async () => {
    const browser = await browseToDefinition()

    await browser.chooseDatabase('Other')

    expect(browser.definition.value).toBeNull()
    expect(browser.selected.value).toBeNull()
  })

  it('clears everything when the database is cleared', async () => {
    const browser = await browseToDefinition()

    await browser.chooseDatabase(null)

    expect(browser.objects.value).toEqual([])
    expect(browser.definition.value).toBeNull()
  })

  it('filters the list by name, case insensitively', async () => {
    const browser = await browseToDefinition()

    browser.filter.value = 'SETTLE'

    expect(browser.visible.value.map((o) => o.name)).toEqual(['sp_settle_bet'])
  })

  it('shows everything when the filter is blank', async () => {
    const browser = await browseToDefinition()

    browser.filter.value = '   '

    expect(browser.visible.value).toHaveLength(2)
  })

  it('drops the filter when the database changes', async () => {
    const browser = await browseToDefinition()
    browser.filter.value = 'settle'

    await browser.chooseDatabase('Other')

    expect(browser.filter.value).toBe('')
  })

  // An absent object is a 404 the operator has to see, not an empty panel.
  it('surfaces a refused definition as a failure', async () => {
    routeGet({ '/schema/databases': ['aviorfixture'], '/objects': objects })
    get.mockImplementation((path: string) => {
      if (path === '/objects') return Promise.resolve(ok(objects))
      if (path === '/objects/definition') {
        return Promise.resolve({
          data: undefined,
          error: { code: 'content.object_unknown', message: 'No object named public.x exists.' },
          response: { status: 404 },
        })
      }
      return Promise.resolve(ok(['aviorfixture']))
    })

    const browser = useObjectBrowser(instanceId, () => 'stored_procedure')
    await browser.chooseDatabase('aviorfixture')
    await browser.choose(memberBets)

    expect(browser.failure.value).toEqual({
      kind: 'error',
      code: 'content.object_unknown',
      message: 'No object named public.x exists.',
    })
    expect(browser.definition.value).toBeNull()
  })

  it('reports an unreachable service rather than failing silently', async () => {
    get.mockRejectedValue(new Error('offline'))
    const browser = useObjectBrowser(instanceId, () => 'view')

    await browser.loadDatabases()

    expect(browser.failure.value?.kind).toBe('error')
    expect(browser.busy.value).toBe(false)
  })

  it('does not ask for a definition with no database chosen', async () => {
    const browser = useObjectBrowser(instanceId, () => 'view')

    await browser.choose({ schema: 'public', name: 'member_view' })

    expect(get).not.toHaveBeenCalled()
    expect(browser.definition.value).toBeNull()
  })

  // The five menu entries share one component instance: Vue Router reuses it
  // across them rather than remounting. A type captured at setup left the
  // heading following the route while the list, its label and the request
  // stayed on whichever entry was opened first.
  it('follows the type when the operator moves between menu entries', async () => {
    routeGet({ '/schema/databases': ['aviorfixture'], '/objects': objects })
    const objectType = ref<'stored_procedure' | 'function'>('stored_procedure')
    const browser = useObjectBrowser(instanceId, () => objectType.value)
    await browser.chooseDatabase('aviorfixture')

    expect(browser.label.value).toBe('Stored Procedure')

    objectType.value = 'function'
    await nextTick()
    await Promise.resolve()

    expect(browser.label.value).toBe('Function')
    expect(get).toHaveBeenLastCalledWith('/objects', {
      params: { query: { instanceId, database: 'aviorfixture', type: 'function' } },
    })
  })

  it('drops the previous type selection when the entry changes', async () => {
    const objectType = ref<'stored_procedure' | 'function'>('stored_procedure')
    routeGet({
      '/schema/databases': ['aviorfixture'],
      '/objects': objects,
      '/objects/definition': definition,
    })
    const browser = useObjectBrowser(instanceId, () => objectType.value)
    await browser.chooseDatabase('aviorfixture')
    await browser.choose(memberBets)
    expect(browser.definition.value).not.toBeNull()

    objectType.value = 'function'
    await nextTick()
    await Promise.resolve()

    expect(browser.definition.value).toBeNull()
    expect(browser.selected.value).toBeNull()
  })

  // An index is read through the table that carries it, and the operator is
  // asked for a table. The predecessor's menu says the same.
  it('labels the index type Table', () => {
    expect(objectTypeLabels.index).toBe('Table')
    expect(useObjectBrowser(instanceId, () => 'index').label.value).toBe('Table')
    expect(useObjectBrowser(instanceId, () => 'view').label.value).toBe('View')
  })

  it('opens where a link points, reading the definition the way a click does', async () => {
    routeGet({ '/objects': objects, '/objects/definition': definition })
    const browser = useObjectBrowser(instanceId, () => 'stored_procedure')
    await browser.openAt('aviorfixture', memberBets)
    expect(browser.database.value).toBe('aviorfixture')
    expect(browser.objects.value).toEqual(objects)
    expect(browser.selected.value).toEqual(memberBets)
    expect(browser.definition.value).toEqual(definition)
    expect(get).toHaveBeenLastCalledWith('/objects/definition', {
      params: {
        query: {
          instanceId,
          database: 'aviorfixture',
          type: 'stored_procedure',
          schema: 'public',
          name: 'sp_member_bets',
        },
      },
    })
  })

  it('opens on a database alone when the link names no object', async () => {
    routeGet({ '/objects': objects })
    const browser = useObjectBrowser(instanceId, () => 'function')
    await browser.openAt('aviorfixture', null)
    expect(browser.objects.value).toEqual(objects)
    expect(browser.selected.value).toBeNull()
    expect(get).not.toHaveBeenCalledWith('/objects/definition', expect.anything())
  })
})
