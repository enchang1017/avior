import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VBeacon from '../VBeacon.vue'

import type { CurrentUser } from '@/shared/api'

function badge(environment: CurrentUser['environment']) {
  return mount(VBeacon, { props: { environment } })
}

describe('VBeacon', () => {
  it('names the deployment', () => {
    expect(badge('production').text()).toBe('PRODUCTION')
    expect(badge('staging').text()).toBe('STAGING')
  })

  /*
   * The class is what the styling hangs on, and the difference between the two
   * is the whole point: production is a red beacon and everything else a quiet
   * pill. A badge that shouted on every deployment would be one nobody read,
   * including on the deployment where it is true.
   */
  it('marks production apart from the rest', () => {
    expect(badge('production').classes()).toContain('production')
    expect(badge('staging').classes()).toContain('staging')
    expect(badge('development').classes()).not.toContain('production')
  })

  // Four letters in a corner are not self-explanatory to somebody seeing them
  // for the first time.
  // The beacon is production's alone: nothing else on the page moves all day.
  it('lights the beacon on production and nowhere else', () => {
    expect(badge('production').find('.animate-beacon-sweep').exists()).toBe(true)
    expect(badge('production').find('.animate-beacon-pulse').exists()).toBe(true)
    expect(badge('staging').find('[class*="animate-"]').exists()).toBe(false)
    expect(badge('development').find('[class*="animate-"]').exists()).toBe(false)
  })

  it('says what it means on hover', () => {
    expect(badge('staging').attributes('title')).toBe('This is the staging deployment')
  })

  // FEAT-065: the query console reads production, and says it only reads.
  it('says read-only on production when the page only reads', () => {
    const console = mount(VBeacon, { props: { environment: 'production', readOnly: true } })
    expect(console.text()).toBe('PRODUCTION· READ-ONLY')
    expect(mount(VBeacon, { props: { environment: 'staging', readOnly: true } }).text()).toBe(
      'STAGING',
    )
  })
})
