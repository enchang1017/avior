import { describe, expect, it } from 'vitest'

import { countText, groupEntries, viaText, type Hop } from './impact'
import { impactSentence } from './impactSentence'
import { impactMarkdown } from './impactMarkdown'
import type { LineageColumnEntry, LineageNode } from './types'

const zero = { writers: 0, readers: 0, views: 0, triggers: 0, dynamic: 0 }

const node = (name: string, kind: LineageNode['kind'], args?: string): LineageNode => ({
  id: `${kind}:${name}`,
  kind,
  schema: 'public',
  name,
  ...(args === undefined ? {} : { arguments: args }),
})

const entry = (
  object: LineageNode,
  access: 'read' | 'write',
  extra: Partial<LineageColumnEntry> = {},
): LineageColumnEntry => ({
  object,
  access,
  confidence: 'parsed',
  kind: access === 'write' ? 'routine_write' : 'routine_read',
  ...extra,
})

describe('impactSentence', () => {
  it('says every part, routines named once', () => {
    expect(
      impactSentence('wallet.balance', {
        writers: 3,
        readers: 5,
        views: 2,
        triggers: 1,
        dynamic: 1,
      }),
    ).toBe(
      'wallet.balance: 3 routines write it, 5 read it, 2 views depend on it, 1 trigger; 1 DYNAMIC routine may reach it.',
    )
  })

  it('uses the singular for one and leaves out a part that is zero', () => {
    expect(impactSentence('bet', { ...zero, writers: 1, views: 1 })).toBe(
      'bet: 1 routine writes it, 1 view depends on it.',
    )
    expect(impactSentence('bet.stake', { ...zero, readers: 2, triggers: 3 })).toBe(
      'bet.stake: 2 routines read it, 3 triggers.',
    )
    expect(impactSentence('bet.note', { ...zero, dynamic: 2 })).toBe(
      'bet.note: 2 DYNAMIC routines may reach it.',
    )
  })

  it('says so when nothing touches it', () => {
    expect(impactSentence('island.id', zero)).toBe(
      'island.id: nothing the database holds reads or writes it.',
    )
  })
})

describe('groupEntries', () => {
  it('groups by kind in a fixed order and leaves out empty groups', () => {
    const settle = entry(node('settle', 'function', 'bigint'), 'write')
    const report = entry(node('report', 'procedure'), 'read')
    const view = entry(node('member_bets', 'view'), 'read', {
      kind: 'view_read',
      confidence: 'exact',
    })
    const policy = entry(node('bet', 'table'), 'read', { kind: 'policy_read', via: 'policy open' })
    expect(
      groupEntries([view, report, policy, settle]).map((group) => [
        group.title,
        group.entries.map((one) => one.object.name),
      ]),
    ).toEqual([
      ['Routines that write', ['settle']],
      ['Routines that read', ['report']],
      ['Views', ['member_bets']],
      ['Policies, defaults and keys', ['bet']],
    ])
    expect(groupEntries([settle]).map((group) => group.title)).toEqual(['Routines that write'])
  })
})

describe('viaText', () => {
  it('names each hop of the chain', () => {
    const hops: Hop[] = [
      { kind: 'view_read', node: node('member_bets', 'view') },
      { kind: 'view_read', node: node('big_open_bets', 'view') },
    ]
    expect(viaText(hops)).toBe('via view public.member_bets → view public.big_open_bets')
    expect(
      viaText([
        { kind: 'trigger', node: node('ledger', 'table'), trigger: 'trg_sync' },
        { kind: 'routine_write', node: node('sync', 'function') },
      ]),
    ).toBe('via trigger trg_sync on public.ledger → function public.sync')
  })
})

describe('impactMarkdown', () => {
  it('copies the sentence, the groups, the triggers and the indirect entries with the cut', () => {
    const settle = entry(node('settle', 'function', 'bigint'), 'write', {
      excerpt: "UPDATE bet SET status = 'settled'",
      line: 14,
    })
    const markdown = impactMarkdown({
      sentence: 'bet.status: 1 routine writes it.',
      groups: groupEntries([settle]),
      triggers: [
        {
          name: 'bet_logged',
          function: node('log_bet', 'function', ''),
          events: ['insert', 'update'],
        },
      ],
      indirect: [
        {
          entry: entry(node('post', 'function', ''), 'write'),
          via: [{ kind: 'trigger', node: node('ledger', 'table'), trigger: 'trg' }],
        },
      ],
      indirectTotal: 21,
    })
    expect(markdown).toBe(
      [
        'bet.status: 1 routine writes it.',
        '',
        '### Routines that write',
        "- `public.settle(bigint)` — function · PARSED · UPDATE bet SET status = 'settled' · line 14",
        '',
        '### Triggers',
        '- `bet_logged` — runs `public.log_bet()` on insert, update',
        '',
        '### Indirect',
        '- `public.post()` — function · PARSED · via trigger trg on public.ledger',
        '',
        '1 of 21 shown.',
        '',
      ].join('\n'),
    )
  })
})

describe('countText', () => {
  it('counts objects, and the statements too when there are more of them', () => {
    const settle = node('settle', 'function', 'bigint')
    const report = node('report', 'function')
    expect(countText([entry(settle, 'read'), entry(report, 'read')])).toBe('2 routines')
    expect(
      countText([
        entry(settle, 'read', { line: 3 }),
        entry(settle, 'read', { line: 9 }),
        entry(report, 'read'),
      ]),
    ).toBe('3 statements in 2 routines')
    expect(countText([entry(node('member_bets', 'view'), 'read')])).toBe('1 view')
    expect(
      countText([entry(node('bet', 'table'), 'read'), entry(node('bet', 'table'), 'write')]),
    ).toBe('2 statements in 1 object')
  })
})
