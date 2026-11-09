import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VReadout from '../VReadout.vue'

describe('VReadout', () => {
  it('reads its parts in order, separated, with the verdict toned', () => {
    const readout = mount(VReadout, {
      props: {
        label: 'Result',
        parts: [
          { value: 'ALLOWED', tone: 'ok' },
          { key: 'ROWS', value: '3' },
        ],
      },
    })
    expect(readout.findAll('.part').map((part) => part.text())).toEqual(['ALLOWED', 'ROWS 3'])
    expect(readout.find('.text-ok').text()).toBe('ALLOWED')
    expect(readout.attributes('aria-label')).toBe('Result')
  })
})
