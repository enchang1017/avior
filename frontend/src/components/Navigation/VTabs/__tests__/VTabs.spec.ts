import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VTabs from '../VTabs.vue'

const tabs = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
]

describe('VTabs', () => {
  it('names the strip and marks the open tab', () => {
    const strip = mount(VTabs, { props: { tabs, label: 'Client', modelValue: 'codex' } })

    expect(strip.attributes('role')).toBe('tablist')
    expect(strip.attributes('aria-label')).toBe('Client')
    expect(strip.findAll('[role="tab"]').map((tab) => tab.attributes('aria-selected'))).toEqual([
      'false',
      'true',
    ])
  })

  it('opens the tab that is chosen', async () => {
    const strip = mount(VTabs, { props: { tabs, label: 'Client', modelValue: 'codex' } })

    await strip.findAll('[role="tab"]')[0]?.trigger('click')
    expect(strip.emitted('update:modelValue')).toEqual([['claude']])
  })
})
