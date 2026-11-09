import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VEmpty from '../VEmpty.vue'

describe('VEmpty', () => {
  // A sentence rather than a blank area: an empty list and a list that failed
  // to load look identical when neither says anything.
  it('says why the list is empty', () => {
    expect(mount(VEmpty, { slots: { default: 'Nothing is waiting.' } }).text()).toBe(
      'Nothing is waiting.',
    )
  })
})
