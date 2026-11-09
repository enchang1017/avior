import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import VDateTime from './VDateTime.vue'

/*
 * The value contract is the whole reason this component can exist quietly: it
 * is the string `datetime-local` writes, so every caller swapped the element
 * for this and changed nothing else. These cases hold that contract, and the
 * two behaviours somebody would notice immediately if they broke.
 */

// A fixed clock, because "today" and "now" are two of the cases.
const noon = new Date(2026, 8, 9, 14, 30, 0)

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(noon)
})

afterEach(() => {
  vi.useRealTimers()
})

function open(modelValue = '') {
  const wrapper = mount(VDateTime, { props: { modelValue, id: 'when' } })
  return wrapper
}

async function opened(modelValue = '') {
  const wrapper = open(modelValue)
  await wrapper.find('button.trigger').trigger('click')
  return wrapper
}

function emitted(wrapper: ReturnType<typeof open>): string[] {
  return (wrapper.emitted('update:modelValue') ?? []).map((call) => String(call[0]))
}

describe('VDateTime', () => {
  it('shows the placeholder until something is chosen', () => {
    const wrapper = mount(VDateTime, {
      props: { modelValue: '', placeholder: 'Any time' },
    })

    expect(wrapper.find('.shown').text()).toBe('Any time')
    expect(wrapper.find('.shown').classes()).toContain('empty')
  })

  it('opens and closes on the trigger', async () => {
    const wrapper = open()
    expect(wrapper.find('.panel').exists()).toBe(false)

    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('.panel').exists()).toBe(true)

    await wrapper.find('button.trigger').trigger('click')
    expect(wrapper.find('.panel').exists()).toBe(false)
  })

  /*
   * Six weeks, always, and the days either side shown rather than blank.
   *
   * A grid that grows a row as somebody pages through months moves the day they
   * were about to click.
   */
  it('draws six weeks whatever month it is on', async () => {
    for (const month of ['2026-02-10T00:00', '2026-08-10T00:00', '2026-11-10T00:00']) {
      const wrapper = await opened(month)
      expect(wrapper.findAll('.day')).toHaveLength(42)
    }
  })

  it('writes the value the way datetime-local does', async () => {
    const wrapper = await opened('2026-09-09T14:30')

    // The 9th is in the third row of a September that starts on a Tuesday; find
    // it by its text rather than by counting.
    const ninth = wrapper.findAll('.day').find((day) => day.text() === '10')
    await ninth?.trigger('click')

    expect(emitted(wrapper)).toEqual(['2026-09-10T14:30'])
  })

  // Somebody narrowing a range by a day should not have their hour reset to
  // midnight under them.
  it('keeps the time already chosen when the day changes', async () => {
    const wrapper = await opened('2026-09-09T07:05')
    const other = wrapper.findAll('.day').find((day) => day.text() === '12')
    await other?.trigger('click')

    expect(emitted(wrapper)).toEqual(['2026-09-12T07:05'])
  })

  it('keeps the day when the time changes, and refuses an hour that is not one', async () => {
    const wrapper = await opened('2026-09-09T07:05')

    await wrapper.find('#when-hours').setValue('21')
    expect(emitted(wrapper)).toEqual(['2026-09-09T21:05'])

    // Out of range is clamped rather than written through: 25 o'clock would
    // become tomorrow, which is not what somebody typing it meant.
    await wrapper.find('#when-hours').setValue('25')
    expect(emitted(wrapper).at(-1)).toBe('2026-09-09T23:05')
  })

  it('offers now and nothing', async () => {
    const wrapper = await opened('2026-01-01T00:00')

    await wrapper.findAll('.foot button')[1]?.trigger('click')
    expect(emitted(wrapper).at(-1)).toBe('2026-09-09T14:30')

    await wrapper.findAll('.foot button')[0]?.trigger('click')
    expect(emitted(wrapper).at(-1)).toBe('')
  })

  // Paging is about looking, not choosing: it must not write a value.
  it('pages through months without choosing anything', async () => {
    const wrapper = await opened('2026-09-09T14:30')
    const back = wrapper.findAll('.head button')[0]

    await back?.trigger('click')
    await back?.trigger('click')

    expect(wrapper.find('.month').text()).not.toBe('')
    expect(emitted(wrapper)).toEqual([])
  })

  it('marks today and the chosen day, and dims the days either side', async () => {
    const wrapper = await opened('2026-09-12T00:00')

    expect(wrapper.findAll('.day.picked')).toHaveLength(1)
    expect(wrapper.find('.day.picked').text()).toBe('12')
    expect(wrapper.find('.day.today').text()).toBe('9')
    expect(wrapper.findAll('.day.outside').length).toBeGreaterThan(0)
  })
})
