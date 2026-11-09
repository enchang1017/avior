import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VSelect from '../VSelect.vue'

const verdicts = [
  { value: '', label: 'Any' },
  { value: 'allow', label: 'Allowed' },
  { value: 'deny', label: 'Refused' },
]

function render(modelValue = '') {
  return mount(VSelect, { props: { id: 'verdict', modelValue, options: verdicts } })
}

function labels(select: ReturnType<typeof render>) {
  return select.findAll('.option:not(.none)').map((option) => option.text())
}

describe('VSelect', () => {
  // The label is what a person reads; the value is what the server is sent.
  // A control showing `allow` where the list said `Allowed` has two names for
  // one thing and makes the reader work out which.
  it('shows the label of what is chosen, not its value', () => {
    expect(render('allow').find('input').attributes('value')).toBe('Allowed')
  })

  it('offers every option when opened', async () => {
    const select = render()
    await select.find('input').trigger('focus')

    expect(labels(select)).toEqual(['Any', 'Allowed', 'Refused'])
  })

  it('reports the value, not the label', async () => {
    const select = render()
    await select.find('input').trigger('focus')
    await select.findAll('.option')[1]?.trigger('mousedown')

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['allow'])
  })

  // The difference from VFilterSelect, and the only thing worth saying about
  // either. A verdict is one of three things: letting somebody type `allowed`
  // would produce a request the server refuses for a reason they would have to
  // guess at.
  it('never reports a value that is not on the list', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')
    await input.setValue('allowed')

    expect(select.emitted('update:modelValue')).toBeUndefined()
  })

  // A column picker over a wide table is a closed set with forty things in it.
  it('filters by what is typed, on the label', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')
    await input.setValue('ref')

    expect(labels(select)).toEqual(['Refused'])
  })

  it('says so when nothing matches, rather than looking broken', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')
    await input.setValue('nothing like this')

    expect(select.find('.option.none').exists()).toBe(true)
  })

  // Half a word is not a choice, so it goes when the list closes rather than
  // sitting in the control as if it were one.
  it('forgets what was typed when the list closes', async () => {
    const select = render('allow')
    const input = select.find('input')
    await input.trigger('focus')
    await input.setValue('ref')
    await input.trigger('keydown', { key: 'Escape' })

    expect(input.attributes('value')).toBe('Allowed')
  })

  it('can be worked without a mouse', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')

    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['deny'])
  })

  it('opens nothing while disabled', () => {
    const select = mount(VSelect, {
      props: { id: 'verdict', modelValue: '', options: verdicts, disabled: true },
    })

    expect(select.find('input').attributes('disabled')).toBeDefined()
    expect(select.find('.caret').attributes('disabled')).toBeDefined()
  })

  // The caret is the affordance somebody who has not realised they can type
  // reaches for, so it has to both open and close.
  it('opens and closes from the caret', async () => {
    const select = render()

    await select.find('.caret').trigger('mousedown')
    expect(select.find('.list').exists()).toBe(true)

    await select.find('.caret').trigger('mousedown')
    expect(select.find('.list').exists()).toBe(false)
  })

  // Deferred by a tick, so that choosing an option is not cancelled by the blur
  // that clicking it causes.
  it('closes when it loses focus', async () => {
    const select = render()
    await select.find('input').trigger('focus')
    expect(select.find('.list').exists()).toBe(true)

    await select.find('input').trigger('blur')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(select.find('.list').exists()).toBe(false)
  })

  // Enter belongs to the form unless an option is highlighted: somebody who
  // typed and pressed enter meant "apply", not "take the first thing".
  it('leaves enter alone when nothing is highlighted', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')
    await input.trigger('keydown', { key: 'Enter' })

    expect(select.emitted('update:modelValue')).toBeUndefined()
  })

  it('wraps around the ends of the list', async () => {
    const select = render()
    const input = select.find('input')
    await input.trigger('focus')

    await input.trigger('keydown', { key: 'ArrowUp' })
    await input.trigger('keydown', { key: 'Enter' })

    expect(select.emitted('update:modelValue')?.at(-1)).toEqual(['deny'])
  })
})
