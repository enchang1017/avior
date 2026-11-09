import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VFilterBar from '../VFilterBar.vue'

describe('VFilterBar', () => {
  // A form rather than inputs in a div, so enter applies the filters — which is
  // what somebody who has just typed a table name expects, and what three views
  // each had to remember separately.
  it('applies its filters when the form is submitted', async () => {
    const bar = mount(VFilterBar)

    await bar.find('form').trigger('submit')

    expect(bar.emitted('submit')).toHaveLength(1)
  })

  it('cannot be applied again while it is already reading', () => {
    const bar = mount(VFilterBar, { props: { busy: true } })

    expect(bar.find('button').attributes('disabled')).toBeDefined()
  })
})
