import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VCheckbox from '../VCheckbox.vue'

describe('VCheckbox', () => {
  it('shows the model and reports a change', async () => {
    const check = mount(VCheckbox, { props: { modelValue: true }, slots: { default: 'Hide' } })
    const input = check.get('input')
    expect((input.element as HTMLInputElement).checked).toBe(true)
    expect(check.text()).toBe('Hide')

    await input.setValue(false)
    expect(check.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('gives the caller its attributes on the input, not on the label', () => {
    const check = mount(VCheckbox, { attrs: { id: 'own', 'data-testid': 'own' } })
    expect(check.get('input').attributes()).toMatchObject({ id: 'own', 'data-testid': 'own' })
    expect(check.attributes('data-testid')).toBeUndefined()
  })

  // Its box is a control's height and sits at the bottom of the row, which is
  // what puts its centre on the selects' centre.
  it('is one control tall and stands at the bottom of a row', () => {
    const classes = mount(VCheckbox).classes()
    expect(classes).toEqual(expect.arrayContaining(['h-control', 'self-end', 'items-center']))
  })
})
