import { describe, expect, it } from 'vitest'

import { environmentTab } from './environment'

describe('environment tint', () => {
  it('reddens production, blues staging and leaves the rest neutral', () => {
    expect(environmentTab('production')).toContain('border-t-env-production')
    expect(environmentTab('staging')).toContain('border-t-env-staging')
    expect(environmentTab('development')).toContain('border-t-env-local')
    expect(environmentTab('test')).toBe(environmentTab('development'))
  })
})
