import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VScopeSearch from '../VScopeSearch.vue'
import { formatQuery, parseQuery, withoutScope } from '../scopes'

const keys = ['database', 'by'] as const

describe('parseQuery', () => {
  it('reads the named keys as scopes and everything else as words', () => {
    expect(parseQuery('database:aviorfixture Bob tier', keys)).toEqual({
      scopes: [{ key: 'database', value: 'aviorfixture' }],
      words: ['bob', 'tier'],
    })
  })

  // A colon in a word is not a scope unless the page asked for that key.
  it('leaves an unknown key, and an empty value, as a word', () => {
    expect(parseQuery('state:x database: a:b', keys)).toEqual({
      scopes: [],
      words: ['state:x', 'database:', 'a:b'],
    })
  })

  it('removes one scope and keeps the rest of the query', () => {
    const query = 'database:ledger by:chen.yu refund'
    expect(withoutScope(query, keys, 0)).toBe('by:chen.yu refund')
    expect(formatQuery(parseQuery('  ', keys))).toBe('')
  })
})

describe('VScopeSearch', () => {
  it('draws each scope as a chip that removes itself', async () => {
    const wrapper = mount(VScopeSearch, {
      props: {
        id: 'q',
        keys,
        placeholder: 'Filter',
        modelValue: 'database:ledger refund',
        'onUpdate:modelValue': (next: string) => wrapper.setProps({ modelValue: next }),
      },
    })
    expect(wrapper.findAll('.scope').map((chip) => chip.text())).toEqual(['database:ledger'])

    await wrapper.find('.scope button').trigger('click')
    expect(wrapper.props('modelValue')).toBe('refund')
    expect(wrapper.findAll('.scope')).toHaveLength(0)
  })
})
