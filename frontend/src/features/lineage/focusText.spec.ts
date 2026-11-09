import { describe, expect, it } from 'vitest'

import { focusSuggestions, focusText, resolveFocusText } from './focusText'
import type { FocusRef } from './types'

const bet: FocusRef = { schema: 'public', name: 'bet', kind: 'table' }
const promoBet: FocusRef = { schema: 'promo', name: 'bet', kind: 'table' }
const wallet: FocusRef = { schema: 'public', name: 'wallet', kind: 'table' }
const memberBets: FocusRef = { schema: 'public', name: 'member_bets', kind: 'view' }
const settle: FocusRef = { schema: 'public', name: 'settle_bet', kind: 'routine' }
const refs = [bet, promoBet, wallet, memberBets, settle]

const columns = new Map<FocusRef, string[]>([
  [bet, ['id', 'status', 'stake']],
  [wallet, ['id', 'balance']],
  [memberBets, ['member_id', 'status']],
])
const columnsOf = (ref: FocusRef) => columns.get(ref)

describe('resolveFocusText', () => {
  it('picks an object by its label, its qualified name, or a bare name only one relation has', () => {
    expect(resolveFocusText('public.member_bets · view', refs, columnsOf)).toEqual({
      ref: memberBets,
      column: null,
    })
    expect(resolveFocusText('public.bet', refs, columnsOf)).toEqual({ ref: bet, column: null })
    expect(resolveFocusText('wallet', refs, columnsOf)).toEqual({ ref: wallet, column: null })
    expect(resolveFocusText('bet', refs, columnsOf)).toBeNull()
  })

  it('picks a column as table.column or schema.table.column, whatever its case', () => {
    expect(resolveFocusText('wallet.balance', refs, columnsOf)).toEqual({
      ref: wallet,
      column: 'balance',
    })
    expect(resolveFocusText(' public.bet.STATUS ', refs, columnsOf)).toEqual({
      ref: bet,
      column: 'status',
    })
    expect(resolveFocusText('public.member_bets.status', refs, columnsOf)).toEqual({
      ref: memberBets,
      column: 'status',
    })
  })

  it('picks nothing for a column the table does not hold, or whose columns are not known', () => {
    expect(resolveFocusText('wallet.owner', refs, columnsOf)).toBeNull()
    expect(resolveFocusText('promo.bet.status', refs, columnsOf)).toBeNull()
    expect(resolveFocusText('nowhere.at_all', refs, columnsOf)).toBeNull()
    expect(resolveFocusText('   ', refs, columnsOf)).toBeNull()
  })
})

describe('focusSuggestions', () => {
  it('offers the objects whose labels hold the text', () => {
    expect(focusSuggestions('bet', refs, columnsOf)).toEqual([
      'public.bet',
      'promo.bet',
      'public.member_bets · view',
      'public.settle_bet · routine',
    ])
  })

  it("offers a table's columns after its name and a dot, narrowed by what follows", () => {
    expect(focusSuggestions('wallet.', refs, columnsOf)).toEqual([
      'public.wallet.id',
      'public.wallet.balance',
    ])
    expect(focusSuggestions('public.bet.st', refs, columnsOf)).toEqual([
      'public.bet.status',
      'public.bet.stake',
    ])
  })
})

describe('focusText', () => {
  it('shows an object by its label and a column as schema.table.column', () => {
    expect(focusText(memberBets, null)).toBe('public.member_bets · view')
    expect(focusText(bet, 'status')).toBe('public.bet.status')
    expect(focusText(null, null)).toBe('')
  })
})
