import { mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import VPalette from '../VPalette.vue'
import { registerPaletteSource, usePalette, type PaletteItem } from '@/shared/palette'

beforeAll(() => {
  const dialog = window.HTMLDialogElement.prototype
  if (typeof dialog.showModal === 'function') return
  dialog.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute('open', '')
  }
  dialog.close = function close(this: HTMLDialogElement) {
    this.removeAttribute('open')
  }
})

async function settle() {
  await nextTick()
  await new Promise((resolve) => setTimeout(resolve, 0))
  await nextTick()
}

describe('VPalette', () => {
  const leave: (() => void)[] = []
  afterEach(() => {
    leave.splice(0).forEach((undo) => undo())
    usePalette().hide()
  })

  // No invented results: with nothing registered it says so.
  it('says there is nothing to search while no source is registered', async () => {
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('member')
    await settle()
    expect(wrapper.find('.empty').text()).toBe('Nothing is searchable here yet.')
    wrapper.unmount()
  })

  it('lists what a source finds and opens the chosen one', async () => {
    const run = vi.fn()
    leave.push(
      registerPaletteSource(() => [
        { id: 'a', label: 'public.member', icon: 'table', run },
        { id: 'b', label: 'public.bet', icon: 'table', run: () => undefined },
      ]),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('pub')
    await settle()

    expect(wrapper.findAll('[role="option"]').map((o) => o.text())).toEqual([
      'public.member',
      'public.bet',
    ])
    await wrapper.find('input').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.find('input').trigger('keydown', { key: 'ArrowUp' })
    await wrapper.find('input').trigger('keydown', { key: 'Enter' })
    expect(run).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('moves between a row’s destinations with the arrows and tab, and takes the chosen one', async () => {
    const query = vi.fn()
    const object = vi.fn()
    const lineage = vi.fn()
    leave.push(
      registerPaletteSource(() => [
        {
          id: 'bet',
          label: 'public.bet',
          icon: 'table',
          run: query,
          actions: [
            { label: 'Query', describe: 'Open public.bet in the query console', run: query },
            { label: 'Object', describe: 'Open public.bet in its object viewer', run: object },
            { label: 'Lineage', describe: 'Open public.bet in Lineage', run: lineage },
          ],
        },
        { id: 'plain', label: 'public.plain', icon: 'table', run: () => undefined },
      ]),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    const input = wrapper.find('input')
    await input.setValue('pub')
    await settle()
    const buttons = wrapper.findAll('button.action')
    expect(buttons.map((one) => [one.text(), one.attributes('aria-label')])).toEqual([
      ['Query', 'Open public.bet in the query console'],
      ['Object', 'Open public.bet in its object viewer'],
      ['Lineage', 'Open public.bet in Lineage'],
    ])
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-0-action-0')

    await input.trigger('keydown', { key: 'ArrowRight' })
    await input.trigger('keydown', { key: 'Tab' })
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-0-action-2')
    await input.trigger('keydown', { key: 'Tab', shiftKey: true })
    await input.trigger('keydown', { key: 'ArrowLeft' })
    await input.trigger('keydown', { key: 'ArrowLeft' })
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-0-action-2')
    await input.trigger('keydown', { key: 'Enter' })
    expect(lineage).toHaveBeenCalledOnce()
    expect(query).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('goes back to the primary on another row, and a click takes the destination clicked', async () => {
    const object = vi.fn()
    const row = vi.fn()
    leave.push(
      registerPaletteSource(() => [
        {
          id: 'bet',
          label: 'public.bet',
          icon: 'table',
          run: row,
          actions: [
            { label: 'Query', describe: 'q', run: row },
            { label: 'Object', describe: 'o', run: object },
          ],
        },
        { id: 'plain', label: 'public.plain', icon: 'table', run: () => undefined },
      ]),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    const input = wrapper.find('input')
    await input.setValue('pub')
    await settle()
    await input.trigger('keydown', { key: 'ArrowRight' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-1')
    await input.trigger('keydown', { key: 'ArrowUp' })
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-0-action-0')

    usePalette().show()
    await wrapper.findAll('button.action')[1]?.trigger('click')
    expect(object).toHaveBeenCalledOnce()
    expect(row).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('tells an empty query from one nothing matches', async () => {
    leave.push(registerPaletteSource(() => []))
    const wrapper = mount(VPalette, { attachTo: document.body })
    await settle()
    expect(wrapper.find('.empty').text()).toBe('Type to search.')
    await wrapper.find('input').setValue('zzz')
    await settle()
    expect(wrapper.find('.empty').text()).toBe('Nothing matches “zzz”.')
    wrapper.unmount()
  })

  const item = (id: string, group?: string): PaletteItem => ({
    id,
    label: id,
    icon: 'table',
    run: () => undefined,
    ...(group === undefined ? {} : { group }),
  })

  it('paints a quick source without waiting for a slow one, under group headings', async () => {
    let release: (value: { items: PaletteItem[]; status?: string }) => void = () => undefined
    leave.push(registerPaletteSource(() => [item('Query', 'Pages')]))
    leave.push(
      registerPaletteSource(
        () =>
          new Promise<{ items: PaletteItem[]; status?: string }>((resolve) => (release = resolve)),
      ),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('qu')
    await settle()
    expect(wrapper.findAll('[role="option"]').map((o) => o.text())).toEqual(['Query'])
    expect(wrapper.findAll('h3').map((h) => h.text())).toEqual(['Pages'])

    release({ items: [item('public.query_log', 'Tables')], status: 'still indexing 2 databases' })
    await settle()
    expect(wrapper.findAll('h3').map((h) => h.text())).toEqual(['Pages', 'Tables'])
    expect(wrapper.find('.status').text()).toBe('still indexing 2 databases')
    wrapper.unmount()
  })

  it('aborts the search before on every keystroke and never paints its answer', async () => {
    const asked: { query: string; signal: AbortSignal; resolve: (items: PaletteItem[]) => void }[] =
      []
    leave.push(
      registerPaletteSource(
        (query, signal) =>
          new Promise<PaletteItem[]>((resolve) => asked.push({ query, signal, resolve })),
      ),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('me')
    await settle()
    expect(wrapper.find('.empty').text()).toBe('Searching…')
    await wrapper.find('input').setValue('mem')
    await settle()
    expect(asked.map((a) => [a.query, a.signal.aborted])).toEqual([
      ['me', true],
      ['mem', false],
    ])
    asked[1]?.resolve([item('newer')])
    asked[0]?.resolve([item('older')])
    await settle()
    expect(wrapper.findAll('[role="option"]').map((o) => o.text())).toEqual(['newer'])
    wrapper.unmount()
  })

  it('aborts what is in flight when it closes', async () => {
    const signals: AbortSignal[] = []
    leave.push(
      registerPaletteSource((_, signal) => {
        signals.push(signal)
        return new Promise<PaletteItem[]>(() => undefined)
      }),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    usePalette().show()
    await settle()
    await wrapper.find('input').setValue('mem')
    await settle()
    usePalette().hide()
    await settle()
    expect(signals[0]?.aborted).toBe(true)
    wrapper.unmount()
  })

  it('is a combobox that names the option the keyboard is on', async () => {
    leave.push(registerPaletteSource(() => [item('a'), item('b')]))
    const wrapper = mount(VPalette, { attachTo: document.body })
    const input = wrapper.find('input')
    expect(input.attributes('role')).toBe('combobox')
    expect(input.attributes('aria-activedescendant')).toBeUndefined()
    await input.setValue('x')
    await settle()
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-0')
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe('palette-option-1')
    expect(wrapper.find('#palette-option-1').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('dialog').attributes('data-testid')).toBe('palette')
    wrapper.unmount()
  })

  it('stays open when a close from before it reopened arrives late', async () => {
    const wrapper = mount(VPalette, { attachTo: document.body })
    const dialog = wrapper.find('dialog')
    usePalette().show()
    await settle()
    await dialog.trigger('close')
    expect(usePalette().open.value).toBe(true)
    ;(dialog.element as HTMLDialogElement).removeAttribute('open')
    await dialog.trigger('close')
    expect(usePalette().open.value).toBe(false)
    wrapper.unmount()
  })

  it('opens when it is mounted after being asked to, as a lazy chunk is', async () => {
    usePalette().show()
    const wrapper = mount(VPalette, { attachTo: document.body })
    await settle()
    expect(wrapper.find('dialog').attributes('open')).toBeDefined()
    wrapper.unmount()
  })

  it('answers a query typed before any source registered, once one does', async () => {
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('mem')
    await settle()
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)

    leave.push(registerPaletteSource((query) => [item(`${query}ber`)]))
    await settle()
    expect(wrapper.findAll('[role="option"]').map((o) => o.text())).toEqual(['member'])
    wrapper.unmount()
  })

  it('asks again, and drops the older answer, when a source joins mid-search', async () => {
    const asked: { signal: AbortSignal; resolve: (items: PaletteItem[]) => void }[] = []
    leave.push(
      registerPaletteSource(
        (_, signal) => new Promise<PaletteItem[]>((resolve) => asked.push({ signal, resolve })),
      ),
    )
    const wrapper = mount(VPalette, { attachTo: document.body })
    await wrapper.find('input').setValue('mem')
    await settle()
    leave.push(registerPaletteSource(() => [item('from memory')]))
    await settle()
    expect(asked).toHaveLength(2)
    expect(asked[0]?.signal.aborted).toBe(true)
    asked[0]?.resolve([item('stale')])
    asked[1]?.resolve([item('fresh')])
    await settle()
    expect(wrapper.findAll('[role="option"]').map((o) => o.text())).toEqual([
      'from memory',
      'fresh',
    ])
    wrapper.unmount()
  })
})
