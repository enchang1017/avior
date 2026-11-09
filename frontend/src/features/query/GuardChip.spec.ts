import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import GuardChip from './GuardChip.vue'
import { readRules, writeRules } from './guardRules'

describe('GuardChip', () => {
  it('counts the rules and lists them by the names a refusal quotes', async () => {
    const wrapper = mount(GuardChip, { props: { rules: readRules }, attachTo: document.body })
    expect(wrapper.text()).toContain(`Guarded · ${readRules.length} rules`)

    await wrapper.find('button.trigger').trigger('click')
    const names = wrapper.findAll('li .font-mono').map((item) => item.text())
    expect(names).toContain('wildcard_select')
    expect(names).toContain('statement_not_read_only')
    wrapper.unmount()
  })

  it('holds a write to the write rules', () => {
    expect(writeRules.map((rule) => rule.rule)).toContain('missing_where_clause')
    expect(new Set(readRules.map((rule) => rule.rule)).size).toBe(readRules.length)
  })
})
