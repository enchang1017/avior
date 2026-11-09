import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VFilterSelect from '../VFilterSelect.vue'

const tables = ['public.bet', 'public.member', 'reporting.bet_daily']

function open(modelValue = '') {
  const select = mount(VFilterSelect, {
    props: { id: 'table', modelValue, options: tables, emptyLabel: 'every table' },
    attachTo: document.body,
  })
  return select
}

function optionsOf(select: ReturnType<typeof open>) {
  return select.findAll('.option:not(.clear):not(.none)').map((option) => option.text())
}

describe('VFilterSelect', () => {
  it('offers everything until something is typed', async () => {
    const select = open()
    await select.find('input').trigger('focus')

    expect(optionsOf(select)).toEqual(tables)
  })

  // A substring rather than a prefix: these names are qualified, and somebody
  // looking for bet means public.bet. A prefix match would offer them nothing.
  it('matches anywhere in the name, not only at the front', async () => {
    const select = open('bet')
    await select.find('input').trigger('focus')

    expect(optionsOf(select)).toEqual(['public.bet', 'reporting.bet_daily'])
  })

  it('ignores case, because the operator is typing and the catalogue is not', async () => {
    const select = open('MEMBER')
    await select.find('input').trigger('focus')

    expect(optionsOf(select)).toEqual(['public.member'])
  })

  // These are filters, not choices from a closed set. A database no row
  // currently mentions is still a reasonable thing to ask about, so typing one
  // is allowed and the list says so rather than looking broken.
  it('keeps a name nothing matches, and says it will still be asked for', async () => {
    const select = open('no_such_table')
    await select.find('input').trigger('focus')

    expect(optionsOf(select)).toEqual([])
    expect(select.find('.option.none').text()).toContain('still be asked for')
  })

  it('reports what was typed', async () => {
    const select = open()
    const input = select.find('input')
    await input.setValue('public.mem')

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['public.mem'])
  })

  it('reports what was chosen, and closes', async () => {
    const select = open()
    await select.find('input').trigger('focus')
    await select.findAll('.option')[0]?.trigger('mousedown')

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['public.bet'])
    expect(select.find('.list').exists()).toBe(false)
  })

  // Clearing is offered only when there is something to clear, and it is not
  // one of the values, so it does not read as one.
  it('offers a way back to everything once something is chosen', async () => {
    const chosen = open('public.bet')
    await chosen.find('input').trigger('focus')
    expect(chosen.find('.option.clear').text()).toBe('every table')

    await chosen.find('.option.clear').trigger('mousedown')
    expect(chosen.emitted('update:modelValue')?.at(-1)).toEqual([''])

    const empty = open()
    await empty.find('input').trigger('focus')
    expect(empty.find('.option.clear').exists()).toBe(false)
  })

  it('can be worked without a mouse', async () => {
    const select = open()
    const input = select.find('input')
    await input.trigger('focus')

    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['public.member'])
  })

  // Enter on typed text belongs to the form: it applies the filter. Only a
  // highlighted option takes it.
  it('leaves enter alone when nothing is highlighted', async () => {
    const select = open('bet')
    const input = select.find('input')
    await input.trigger('focus')
    await input.trigger('keydown', { key: 'Enter' })

    expect(select.emitted('update:modelValue')).toBeUndefined()
  })

  it('says a choice was picked, from the list or with enter on what was typed', async () => {
    const listed = open()
    await listed.find('input').trigger('focus')
    await listed.findAll('.option:not(.clear):not(.none)')[1]?.trigger('mousedown')
    expect(listed.emitted('pick')).toEqual([['public.member']])

    const typed = open('bet.status')
    await typed.find('input').trigger('keydown', { key: 'Enter' })
    expect(typed.emitted('pick')).toEqual([['bet.status']])
  })

  it('shows the options as given when the caller has matched them already', async () => {
    const select = mount(VFilterSelect, {
      props: {
        id: 'table',
        modelValue: 'public.bet',
        options: tables,
        emptyLabel: 'every table',
        matchTyped: false,
      },
      attachTo: document.body,
    })
    await select.find('input').trigger('focus')
    expect(optionsOf(select)).toEqual(tables)
  })

  it('closes on escape', async () => {
    const select = open()
    const input = select.find('input')
    await input.trigger('focus')
    expect(select.find('.list').exists()).toBe(true)

    await input.trigger('keydown', { key: 'Escape' })
    expect(select.find('.list').exists()).toBe(false)
  })
})
