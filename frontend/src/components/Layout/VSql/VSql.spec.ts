import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import VSql from './VSql.vue'

// CodeMirror arrives through a dynamic import. Waiting for that import, not a
// wall-clock deadline, is what keeps the first case in a file from failing
// whenever a loaded worker takes longer than a second to transform it.
async function loaded(wrapper: VueWrapper): Promise<void> {
  await vi.dynamicImportSettled()
  await flushPromises()
  expect(wrapper.find('.cm-editor').exists()).toBe(true)
}

/*
 * jsdom has no layout, so what a unit test can answer about a CodeMirror view
 * is narrow: that it mounted, that it holds the statement it was given, and
 * that it refuses to be typed into. How it looks is the browser's answer, and
 * the e2e case asks it there.
 */
describe('VSql', () => {
  // Before the editor has loaded as well as after, so nothing waits on it.
  it('shows the statement it was given', async () => {
    const wrapper = mount(VSql, { props: { code: 'SELECT 1 FROM public.member' } })
    expect(wrapper.text()).toContain('SELECT 1 FROM public.member')

    await loaded(wrapper)
    expect(wrapper.text()).toContain('SELECT 1 FROM public.member')
  })

  // A block somebody's keystrokes vanish into is worse than a plain <pre>: it
  // looks like an input and behaves like a wall.
  it('is read-only, so it reads as text rather than as an input', async () => {
    const wrapper = mount(VSql, { props: { code: 'DELETE FROM public.bet WHERE bet_id = 1' } })
    await loaded(wrapper)
    const content = wrapper.find('.cm-content')

    expect(content.attributes('contenteditable')).not.toBe('true')
  })

  it('replaces the statement rather than remounting when it changes', async () => {
    const wrapper = mount(VSql, { props: { code: 'SELECT 1' } })
    await loaded(wrapper)
    const first = wrapper.find('.cm-editor').element

    await wrapper.setProps({ code: 'SELECT 2' })

    expect(wrapper.text()).toContain('SELECT 2')
    // The same view: a reader who scrolled a long statement keeps their place.
    expect(wrapper.find('.cm-editor').element).toBe(first)
  })

  it('wraps long lines unless told to keep them, and sets no height when it is the page', async () => {
    const wrapped = mount(VSql, { props: { code: 'SELECT 1' } })
    await loaded(wrapped)
    expect(wrapped.find('.cm-content').classes()).toContain('cm-lineWrapping')
    expect(wrapped.classes()).toContain('[&_.cm-editor]:max-h-72')

    const kept = mount(VSql, { props: { code: 'SELECT 1', wrap: false, size: 'full' } })
    expect(kept.find('pre').classes()).toContain('whitespace-pre')
    await loaded(kept)
    expect(kept.find('.cm-content').classes()).not.toContain('cm-lineWrapping')
    expect(kept.classes().some((name) => name.includes('max-h'))).toBe(false)
  })
})
