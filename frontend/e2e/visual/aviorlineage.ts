import type { components } from '@/gen/api'

/**
 * The local stack's lineage demo, public.round at two hops, as the server
 * answered it: seventeen objects whose longest names run off a chart that does
 * not make room for them.
 */
export const roundTwoHops: components['schemas']['LineageGraph'] = {
  edges: [
    {
      access: 'read',
      confidence: 'parsed',
      from: 'function:"public"."log_round"()',
      kind: 'routine_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'function:"public"."log_round"()',
      kind: 'routine_write',
      to: 'table:"public"."round_audit"',
      toColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'routine_write',
      to: 'table:"public"."ledger"',
      toColumns: [
        {
          name: 'amount',
          restricted: false,
        },
        {
          name: 'note',
          restricted: false,
        },
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'routine_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'routine_write',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'stake',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'routine_read',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'balance',
          restricted: false,
        },
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'routine_write',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'balance',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'function:"public"."player_email"(p_player integer)',
      kind: 'routine_read',
      to: 'table:"public"."player"',
      toColumns: [
        {
          name: 'email',
          restricted: false,
        },
        {
          name: 'player_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'function:"public"."void_round"(p_round bigint)',
      kind: 'routine_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'exact',
      from: 'function:"public"."void_round"(p_round bigint)',
      kind: 'routine_write',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'matview:"public"."player_totals"',
      kind: 'view_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'net',
          restricted: false,
        },
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'stake',
          restricted: false,
        },
      ],
    },
    {
      access: 'reference',
      confidence: 'parsed',
      from: 'procedure:"public"."nightly_close"()',
      kind: 'call',
      to: 'function:"public"."archive_table"(p_table text)',
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'procedure:"public"."nightly_close"()',
      kind: 'routine_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'stake',
          restricted: false,
        },
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'procedure:"public"."nightly_close"()',
      kind: 'routine_write',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'routine_write',
      to: 'table:"public"."ledger"',
      toColumns: [
        {
          name: 'amount',
          restricted: false,
        },
        {
          name: 'note',
          restricted: false,
        },
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'routine_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'round_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'routine_write',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'payout',
          restricted: false,
        },
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'parsed',
      from: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'routine_read',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'balance',
          restricted: false,
        },
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'parsed',
      from: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'routine_write',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'balance',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'table:"public"."ledger"',
      fromColumns: [
        {
          name: 'entry_id',
          restricted: false,
        },
      ],
      kind: 'default',
      to: 'sequence:"public"."ledger_entry_id_seq"',
    },
    {
      access: 'reference',
      confidence: 'exact',
      from: 'table:"public"."ledger"',
      fromColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
      ],
      kind: 'foreign_key',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'reference',
      confidence: 'exact',
      from: 'table:"public"."ledger"',
      fromColumns: [
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
      kind: 'foreign_key',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'wallet_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'write',
      confidence: 'exact',
      from: 'table:"public"."player"',
      kind: 'foreign_key',
      to: 'table:"public"."wallet"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'reference',
      confidence: 'exact',
      events: ['insert', 'update'],
      from: 'table:"public"."round"',
      fromColumns: [
        {
          name: 'status',
          restricted: false,
        },
      ],
      kind: 'trigger',
      to: 'function:"public"."log_round"()',
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'table:"public"."round"',
      fromColumns: [
        {
          name: 'round_id',
          restricted: false,
        },
      ],
      kind: 'default',
      to: 'sequence:"public"."round_round_id_seq"',
    },
    {
      access: 'reference',
      confidence: 'exact',
      from: 'table:"public"."round"',
      fromColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
      ],
      kind: 'foreign_key',
      to: 'table:"public"."player"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'reference',
      confidence: 'exact',
      from: 'table:"public"."wallet"',
      fromColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
      ],
      kind: 'foreign_key',
      to: 'table:"public"."player"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'view:"public"."big_open_rounds"',
      kind: 'view_read',
      to: 'view:"public"."open_rounds"',
      toColumns: [
        {
          name: 'name',
          restricted: false,
        },
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'stake',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'view:"public"."open_rounds"',
      kind: 'view_read',
      to: 'table:"public"."player"',
      toColumns: [
        {
          name: 'name',
          restricted: false,
        },
        {
          name: 'player_id',
          restricted: false,
        },
      ],
    },
    {
      access: 'read',
      confidence: 'exact',
      from: 'view:"public"."open_rounds"',
      kind: 'view_read',
      to: 'table:"public"."round"',
      toColumns: [
        {
          name: 'player_id',
          restricted: false,
        },
        {
          name: 'round_id',
          restricted: false,
        },
        {
          name: 'stake',
          restricted: false,
        },
        {
          name: 'status',
          restricted: false,
        },
      ],
    },
  ],
  extensionsHidden: 0,
  focus: 'table:"public"."round"',
  nodes: [
    {
      hop: 0,
      id: 'table:"public"."round"',
      kind: 'table',
      name: 'round',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'function:"public"."log_round"()',
      kind: 'function',
      name: 'log_round',
      schema: 'public',
    },
    {
      arguments: 'p_player integer, p_stake numeric',
      hop: 1,
      id: 'function:"public"."place_round"(p_player integer, p_stake numeric)',
      kind: 'function',
      name: 'place_round',
      schema: 'public',
    },
    {
      arguments: 'p_round bigint',
      hop: 1,
      id: 'function:"public"."void_round"(p_round bigint)',
      kind: 'function',
      name: 'void_round',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'matview:"public"."player_totals"',
      kind: 'matview',
      name: 'player_totals',
      schema: 'public',
    },
    {
      badges: ['may_reach_unknown'],
      hop: 1,
      id: 'procedure:"public"."nightly_close"()',
      kind: 'procedure',
      name: 'nightly_close',
      schema: 'public',
    },
    {
      arguments: 'IN p_round bigint, IN p_payout numeric',
      hop: 1,
      id: 'procedure:"public"."settle_round"(IN p_round bigint, IN p_payout numeric)',
      kind: 'procedure',
      name: 'settle_round',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'sequence:"public"."round_round_id_seq"',
      kind: 'sequence',
      name: 'round_round_id_seq',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'table:"public"."ledger"',
      kind: 'table',
      name: 'ledger',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'table:"public"."player"',
      kind: 'table',
      name: 'player',
      schema: 'public',
    },
    {
      hop: 1,
      id: 'view:"public"."open_rounds"',
      kind: 'view',
      name: 'open_rounds',
      schema: 'public',
    },
    {
      arguments: 'p_table text',
      badges: ['dynamic'],
      hop: 2,
      id: 'function:"public"."archive_table"(p_table text)',
      kind: 'function',
      name: 'archive_table',
      schema: 'public',
    },
    {
      arguments: 'p_player integer',
      hop: 2,
      id: 'function:"public"."player_email"(p_player integer)',
      kind: 'function',
      name: 'player_email',
      schema: 'public',
    },
    {
      hop: 2,
      id: 'sequence:"public"."ledger_entry_id_seq"',
      kind: 'sequence',
      name: 'ledger_entry_id_seq',
      schema: 'public',
    },
    {
      hop: 2,
      id: 'table:"public"."round_audit"',
      kind: 'table',
      name: 'round_audit',
      schema: 'public',
    },
    {
      hop: 2,
      id: 'table:"public"."wallet"',
      kind: 'table',
      name: 'wallet',
      schema: 'public',
    },
    {
      hop: 2,
      id: 'view:"public"."big_open_rounds"',
      kind: 'view',
      name: 'big_open_rounds',
      schema: 'public',
    },
  ],
  snapshot: {
    builtAt: '2026-09-26T10:00:00Z',
    dynamic: 1,
    opaque: 0,
    parsed: 0,
    reused: 9,
    stale: false,
    unparsed: 0,
  },
  state: 'ready',
  total: {
    edges: 30,
    nodes: 17,
  },
  truncated: false,
}
