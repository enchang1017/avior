import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import { maxTabs, readTabs, tooMany, useEditorTabs } from './useEditorTabs'
import { forgetAllTabs } from '@/shared/tabStorage'

describe('editor tabs', () => {
  beforeEach(() => window.localStorage.clear())

  it('starts with one empty tab', () => {
    const { tabs, active } = useEditorTabs('ada')
    expect(tabs.value).toHaveLength(1)
    expect(active.value.statement).toBe('')
    expect(active.value.database).toBeNull()
  })

  // AC-5: each tab keeps its own statement and database.
  it('keeps each tab its own statement and database', () => {
    const editor = useEditorTabs('ada')
    editor.update({ statement: 'SELECT 1', database: 'ledger' })
    const first = editor.activeId.value
    editor.open()
    editor.update({ statement: 'SELECT 2', database: 'aviorfixture' })
    editor.select(first)
    expect(editor.active.value).toMatchObject({ statement: 'SELECT 1', database: 'ledger' })
  })

  it('survives a reload in the same browser', async () => {
    const editor = useEditorTabs('ada')
    editor.update({ statement: 'SELECT kept', database: 'ledger' })
    editor.open()
    await nextTick()
    const again = readTabs('ada')
    expect(again.tabs).toHaveLength(2)
    expect(again.tabs[0]).toMatchObject({ statement: 'SELECT kept', database: 'ledger' })
    expect(again.active).toBe(editor.activeId.value)
  })

  // AC-5: a ninth is refused, and the operator is told why.
  it('refuses a ninth tab with the reason', () => {
    const editor = useEditorTabs('ada')
    for (let open = 1; open < maxTabs; open += 1) expect(editor.open()).toBe(true)
    expect(editor.tabs.value).toHaveLength(maxTabs)
    expect(editor.open()).toBe(false)
    expect(editor.tabs.value).toHaveLength(maxTabs)
    expect(editor.refusal.value).toBe(tooMany)
  })

  it('closes a tab to the one beside it, and never to none', () => {
    const editor = useEditorTabs('ada')
    const first = editor.activeId.value
    editor.open()
    editor.close(editor.activeId.value)
    expect(editor.activeId.value).toBe(first)
    editor.close(first)
    expect(editor.tabs.value).toHaveLength(1)
    expect(editor.activeId.value).not.toBe(first)
  })

  it('reads nonsense and unavailable storage as one empty tab', () => {
    window.localStorage.setItem('avior.tabs.ada', '{"tabs":[{"id":3}]}')
    expect(readTabs('ada').tabs).toHaveLength(1)
    const denied = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(readTabs('ada').tabs[0]?.statement).toBe('')
    denied.mockRestore()
  })

  it("keeps one operator's statements out of another's", async () => {
    const ada = useEditorTabs('ada')
    ada.update({ statement: 'SELECT secret', database: 'ledger' })
    await nextTick()
    expect(readTabs('grace').tabs).toHaveLength(1)
    expect(readTabs('grace').tabs[0]).toMatchObject({ statement: '', database: null })
    expect(readTabs('ada').tabs[0]).toMatchObject({ statement: 'SELECT secret' })
  })

  it('drops tabs aimed at a database the caller no longer reaches', () => {
    const editor = useEditorTabs('ada')
    editor.update({ statement: 'SELECT 1', database: 'ledger' })
    editor.open()
    editor.update({ statement: 'SELECT 2', database: 'aviorfixture' })
    editor.keepReachable(['aviorfixture'])
    expect(editor.tabs.value.map((tab) => tab.database)).toEqual(['aviorfixture'])
    expect(editor.active.value.statement).toBe('SELECT 2')

    editor.keepReachable([])
    expect(editor.tabs.value).toHaveLength(1)
    expect(editor.active.value).toMatchObject({ statement: '', database: null })
  })

  it("forgets every operator's statements, and nothing else", async () => {
    window.localStorage.setItem('avior.tabs', '{}')
    window.localStorage.setItem('avior.theme', 'light')
    useEditorTabs('ada').update({ statement: 'SELECT 1' })
    useEditorTabs('grace').update({ statement: 'SELECT 2' })
    await nextTick()
    forgetAllTabs()
    expect(Object.keys(window.localStorage)).toEqual(['avior.theme'])
  })
})
