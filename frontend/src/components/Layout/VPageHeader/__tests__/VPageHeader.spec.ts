import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VPageHeader from '../VPageHeader.vue'

describe('VPageHeader', () => {
  it('names the page beside its icon', () => {
    const header = mount(VPageHeader, { props: { icon: 'release', title: 'Waiting' } })

    expect(header.find('h1').text()).toBe('Waiting')
    expect(header.find('.tile svg').attributes('data-icon')).toBe('release')
  })

  // One count and one short line, and only when the page has them to give.
  it('shows a count and a subtitle only when given', () => {
    const bare = mount(VPageHeader, { props: { icon: 'release', title: 'Waiting' } })
    expect(bare.find('.count').exists()).toBe(false)
    expect(bare.find('.subtitle').exists()).toBe(false)

    const full = mount(VPageHeader, {
      props: { icon: 'release', title: 'Waiting', count: 0, subtitle: 'Newest first' },
    })
    expect(full.find('.count').text()).toBe('0')
    expect(full.find('.subtitle').text()).toBe('Newest first')
  })

  // The busy indicator was a different word on every page it was copied to.
  it('says nothing about reading until it is reading', () => {
    const idle = mount(VPageHeader, { props: { icon: 'release' } })
    expect(idle.find('svg[data-icon="loader"]').exists()).toBe(false)

    const busy = mount(VPageHeader, { props: { icon: 'release', busy: true } })
    expect(busy.find('svg[data-icon="loader"]').exists()).toBe(true)
    expect(busy.text()).toContain('Reading')
  })

  // The object viewer's heading is a qualified name whose schema is set apart
  // from it, which a string prop cannot express.
  it('lets a page render its own heading', () => {
    const header = mount(VPageHeader, {
      props: { icon: 'eye', title: 'ignored' },
      slots: { title: '<h1 class="name">public.member_view</h1>' },
    })

    expect(header.find('h1').text()).toBe('public.member_view')
    expect(header.text()).not.toContain('ignored')
  })
})
