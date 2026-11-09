import { describe, expect, it } from 'vitest'

import type { components } from '@/gen/api'
import { databaseCard, emphasis, laneTurns, moreCard, rolesMap } from './rolesMap'

type Role = components['schemas']['ProvisionedRole']

const grant = (extra: Partial<components['schemas']['DatabaseGrantSummary']> = {}) => ({
  database: 'shop',
  tables: 3,
  select: 0,
  insert: 0,
  update: 0,
  delete: 0,
  canConnect: true,
  owned: 0,
  ...extra,
})

const role = (name: string, kind: Role['kind'], extra: Partial<Role> = {}): Role => ({
  name,
  kind,
  canLogin: kind === 'login',
  memberOf: [],
  members: [],
  grants: [grant()],
  ...extra,
})

const roles: Role[] = [
  role('shop_app', 'login', { memberOf: ['role_shop', 'role_report'] }),
  role('role_shop', 'group', { members: ['shop_app'], grants: [grant({ select: 3 })] }),
  role('role_report', 'group', {
    members: ['shop_app'],
    grants: [grant({ canConnect: false, select: 1 })],
  }),
  role('role_empty', 'group'),
  role('shop_schema_owner', 'owner', { grants: [grant({ owned: 3, select: 3 })] }),
]

/*
 * FEAT-076 AC-17. The three columns and the lines between them.
 */
describe('rolesMap', () => {
  const map = rolesMap(roles, 'shop')

  it('puts the owner first, then the groups by name, and the logins on the right', () => {
    expect(map.roles.map((card) => card.name)).toEqual([
      'shop_schema_owner',
      'role_empty',
      'role_report',
      'role_shop',
    ])
    expect(map.logins.map((card) => card.name)).toEqual(['shop_app'])
  })

  it('says what the owner owns, and whether each group connects and whom it carries', () => {
    const status = Object.fromEntries(map.roles.map((card) => [card.name, card.status]))
    expect(status).toEqual({
      shop_schema_owner: 'owns 3 tables',
      role_empty: '✓ connect · no login',
      role_report: '⚠ cannot connect · 1 member',
      role_shop: '✓ connect · 1 member',
    })
    expect(map.roles.find((card) => card.name === 'role_report')?.warn).toBe(true)
  })

  it('draws a login in two groups once, with a line to each', () => {
    expect(map.logins).toHaveLength(1)
    expect(map.links.filter((link) => link.to === 'shop_app')).toEqual([
      { from: 'role_shop', to: 'shop_app', dashed: false },
      { from: 'role_report', to: 'shop_app', dashed: false },
    ])
  })

  it('joins the database solid to what may connect and dashed to what only holds privileges', () => {
    const fromDatabase = map.links.filter((link) => link.from === databaseCard)
    expect(fromDatabase).toEqual([
      { from: databaseCard, to: 'shop_schema_owner', dashed: false },
      { from: databaseCard, to: 'role_empty', dashed: false },
      { from: databaseCard, to: 'role_report', dashed: true },
      { from: databaseCard, to: 'role_shop', dashed: false },
    ])
  })

  it('joins a login with no group on the map to the database itself', () => {
    const lone = rolesMap(
      [
        role('direct_app', 'login'),
        role('stray_app', 'login', { grants: [grant({ canConnect: false, select: 1 })] }),
      ],
      'shop',
    )
    expect(lone.links).toEqual([
      { from: databaseCard, to: 'direct_app', dashed: false },
      { from: databaseCard, to: 'stray_app', dashed: true },
    ])
    expect(lone.logins.find((card) => card.name === 'stray_app')?.status).toBe('⚠ cannot connect')
  })

  it('reads a role as unable to connect when the database is missing from its grants', () => {
    const card = rolesMap([role('role_x', 'group', { grants: [] })], 'shop').roles[0]
    expect(card?.canConnect).toBe(false)
    expect(card?.status).toBe('⚠ cannot connect · no login')
  })

  it('gives a crowded group six login cards and one "+N logins" card for the rest', () => {
    const crowd = Array.from({ length: 40 }, (_, i) =>
      role(`svc_${String(i).padStart(2, '0')}`, 'login', { memberOf: ['role_shop'] }),
    )
    const crowded = rolesMap([...roles, ...crowd], 'shop')
    const more = crowded.logins.find((card) => card.kind === 'more')

    expect(crowded.logins).toHaveLength(7)
    expect(more).toMatchObject({
      id: moreCard('role_shop'),
      name: '+35 logins',
      group: 'role_shop',
    })
    expect(crowded.links).toContainEqual({
      from: 'role_shop',
      to: moreCard('role_shop'),
      dashed: false,
    })
  })

  it('keeps a group of seven logins whole', () => {
    const seven = Array.from({ length: 6 }, (_, i) =>
      role(`svc_${i}`, 'login', { memberOf: ['role_shop'] }),
    )
    const whole = rolesMap([...roles, ...seven], 'shop')
    expect(whole.logins.some((card) => card.kind === 'more')).toBe(false)
  })
})

describe('laneTurns', () => {
  it('gives each source card its own lane in a gap, top card first', () => {
    const turns = laneTurns([
      { key: 'b>x', from: 'b', x1: 100, y1: 80, x2: 160 },
      { key: 'a>x', from: 'a', x1: 100, y1: 20, x2: 160 },
      { key: 'a>y', from: 'a', x1: 100, y1: 20, x2: 160 },
    ])
    expect(turns.get('a>x')).toBe(120)
    expect(turns.get('a>y')).toBe(120)
    expect(turns.get('b>x')).toBe(140)
  })

  it('keeps the lanes of one gap apart from another', () => {
    const turns = laneTurns([
      { key: 'db>a', from: 'db', x1: 0, y1: 10, x2: 60 },
      { key: 'a>x', from: 'a', x1: 100, y1: 20, x2: 160 },
    ])
    expect(turns.get('db>a')).toBe(30)
    expect(turns.get('a>x')).toBe(130)
  })
})

describe('emphasis', () => {
  const link = { from: 'role_shop', to: 'shop_ops', dashed: false }
  it('lights the lines of the card pointed at and fades the rest', () => {
    expect(emphasis(link, 'shop_ops')).toBe('lit')
    expect(emphasis(link, 'role_shop')).toBe('lit')
    expect(emphasis(link, 'role_report')).toBe('dim')
  })
  it('leaves every line plain when nothing is pointed at', () => {
    expect(emphasis(link, null)).toBeUndefined()
  })
})
