import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

import SqlScript from './SqlScript.vue'

// CodeMirror arrives through a dynamic import. Waiting for that import, not a
// wall-clock deadline, is what keeps the first case in a file from failing
// whenever a loaded worker takes longer than a second to transform it.
async function loaded(wrapper: VueWrapper): Promise<void> {
  await vi.dynamicImportSettled()
  await flushPromises()
  expect(wrapper.find('.cm-editor').exists()).toBe(true)
}

describe('SqlScript', () => {
  it('renders the script with its keywords marked up', async () => {
    const wrapper = mount(SqlScript, {
      props: { code: "SELECT bet_id, stake FROM public.bet WHERE status = 'Open';" },
      attachTo: document.body,
    })
    await loaded(wrapper)

    expect(wrapper.text()).toContain('SELECT')
    // Plain text would carry no spans at all; the highlighter's presence is
    // what tells a keyword from an identifier.
    expect(wrapper.find('.cm-editor').exists()).toBe(true)
    expect(wrapper.findAll('.ͼ1, .cm-line span').length).toBeGreaterThan(0)

    wrapper.unmount()
  })

  // A caret blinking in text that cannot be edited invites the operator to type.
  it('is not editable', async () => {
    const wrapper = mount(SqlScript, {
      props: { code: 'SELECT 1' },
      attachTo: document.body,
    })
    await loaded(wrapper)

    const content = wrapper.find('.cm-content')
    expect(content.attributes('contenteditable')).not.toBe('true')

    wrapper.unmount()
  })

  it('replaces the document when the code changes', async () => {
    const wrapper = mount(SqlScript, {
      props: { code: 'SELECT 1' },
      attachTo: document.body,
    })
    await loaded(wrapper)

    await wrapper.setProps({ code: 'SELECT 2' })

    expect(wrapper.text()).toContain('SELECT 2')
    expect(wrapper.text()).not.toContain('SELECT 1')

    wrapper.unmount()
  })
})
