import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VEditorTab from '../VEditorTab.vue'

describe('VEditorTab', () => {
  it('takes the environment tint when it is the open one', () => {
    const open = mount(VEditorTab, {
      props: { label: 'ledger', environment: 'production', active: true },
    })
    expect(open.classes()).toEqual(expect.arrayContaining(['active', 'border-t-env-production']))
    expect(open.find('[role="tab"]').attributes('aria-selected')).toBe('true')

    const other = mount(VEditorTab, {
      props: { label: 'ledger', environment: 'production', active: false },
    })
    expect(other.classes()).not.toContain('border-t-env-production')
  })

  it('selects and closes', async () => {
    const tab = mount(VEditorTab, {
      props: { label: 'ledger', environment: 'staging', active: false },
    })
    await tab.find('[role="tab"]').trigger('click')
    await tab.find('button.close').trigger('click')
    expect(tab.emitted('select')).toHaveLength(1)
    expect(tab.emitted('close')).toHaveLength(1)
    expect(tab.find('button.close').attributes('aria-label')).toBe('Close ledger')
  })
})
