import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'

import VInput from '../VInput.vue'

describe('VInput', () => {
  it('shows the model and reports what is typed', async () => {
    const input = mount(VInput, { props: { modelValue: 'alice' } })
    expect((input.element as HTMLInputElement).value).toBe('alice')

    await input.setValue('bob')
    expect(input.emitted('update:modelValue')?.at(-1)).toEqual(['bob'])
  })

  // The caller's attributes are the input's: a label's `for` has to find the
  // id, and a number box has to be one.
  it('passes every attribute through to the input', () => {
    const input = mount(VInput, {
      attrs: { id: 'top', type: 'number', min: '1', placeholder: 'rows' },
    })
    expect(input.attributes()).toMatchObject({
      id: 'top',
      type: 'number',
      min: '1',
      placeholder: 'rows',
    })
  })

  it('keeps v-model.number a number', async () => {
    const host = mount(
      defineComponent({
        components: { VInput },
        setup: () => ({ top: ref<number>(100) }),
        template: '<VInput v-model.number="top" type="number" />',
      }),
    )
    await host.find('input').setValue('250')
    expect((host.vm as unknown as { top: unknown }).top).toBe(250)
  })
})
