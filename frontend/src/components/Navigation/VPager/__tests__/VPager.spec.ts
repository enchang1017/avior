import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VPager from '../VPager.vue'

const onFirstPage = { page: 1, canGoBack: false, hasMore: true }

describe('VPager', () => {
  it('cannot step back from the first page, or past the last', () => {
    const first = mount(VPager, { props: onFirstPage })
    expect(first.findAll('button')[0]?.attributes('disabled')).toBeDefined()
    expect(first.findAll('button')[1]?.attributes('disabled')).toBeUndefined()

    const last = mount(VPager, { props: { page: 3, canGoBack: true, hasMore: false } })
    expect(last.findAll('button')[0]?.attributes('disabled')).toBeUndefined()
    expect(last.findAll('button')[1]?.attributes('disabled')).toBeDefined()
  })

  // A second request while the first is in flight pages twice from one click.
  it('steps nowhere while a page is being read', () => {
    const pager = mount(VPager, { props: { page: 2, canGoBack: true, hasMore: true, busy: true } })

    for (const button of pager.findAll('button')) {
      expect(button.attributes('disabled')).toBeDefined()
    }
  })

  it('asks its list to move rather than moving anything itself', async () => {
    const pager = mount(VPager, { props: { page: 2, canGoBack: true, hasMore: true } })

    await pager.findAll('button')[0]?.trigger('click')
    await pager.findAll('button')[1]?.trigger('click')

    expect(pager.emitted('previous')).toHaveLength(1)
    expect(pager.emitted('next')).toHaveLength(1)
  })

  // The audit trail says how many rows of how many it is showing; the release
  // lists say only which page. Flattening them to the shorter one would drop
  // what somebody paging through an audit trail is reading.
  it('says which page unless the list has more to say', () => {
    expect(mount(VPager, { props: onFirstPage }).text()).toContain('Page 1')
    expect(
      mount(VPager, { props: onFirstPage, slots: { default: '25 of 50 per page' } }).text(),
    ).toContain('25 of 50 per page')
  })
})
