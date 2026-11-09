<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { VBanner, VButton, VInput, VPageHeader, VSkeleton, VTable } from '@/components'
import { describeFailure } from '@/shared/api'
import { useTokens, type ApiToken } from './useTokens'
import { fullStamp, stamp } from '@/shared/timestamp'

/**
 * API tokens. FEAT-053.
 *
 * One rule shapes the page: the value is shown exactly once. The panel that
 * shows it is the only place it ever appears, there is no button that shows it
 * again because the server cannot, and dismissing the panel is final. Somebody
 * who loses it makes another — which is the point, because a token that could be
 * read back is one that could be read back by somebody else.
 */
const store = useTokens()
const name = ref('')
const days = ref(7)
const copied = ref(false)

onMounted(() => void store.load())

const canMint = computed(() => name.value.trim() !== '' && days.value >= 1 && days.value <= 90)

async function mint(): Promise<void> {
  const ok = await store.mint(name.value.trim(), days.value)
  if (ok) {
    name.value = ''
    copied.value = false
  }
}

async function copyMinted(): Promise<void> {
  if (store.minted.value === null) return
  try {
    await navigator.clipboard.writeText(store.minted.value.token)
    copied.value = true
  } catch {
    copied.value = false
  }
}

/*
 * A live token is the good news and an expired one the warning; a revoked one
 * is neither, and reads in the row's own muted colour.
 */
const badgeTone: Record<'revoked' | 'expired' | 'live', string> = {
  live: 'border-ok-line bg-ok-bg text-ok',
  expired: 'border-warn-line bg-warn-bg text-warn',
  revoked: 'border-line-strong',
}

function state(token: ApiToken): 'revoked' | 'expired' | 'live' {
  if (token.revokedAt !== undefined && token.revokedAt !== null) return 'revoked'
  if (new Date(token.expiresAt).getTime() < Date.now()) return 'expired'
  return 'live'
}

function shown(value: string | null | undefined): string {
  return value === undefined || value === null ? '—' : stamp(value)
}

function titled(value: string | null | undefined): string {
  return value === undefined || value === null ? '' : fullStamp(value)
}
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="key"
      title="API tokens"
      subtitle="Read-only. A token cannot write, approve or make another token."
      :busy="store.busy.value"
    />

    <div class="min-h-0 flex-1 overflow-auto px-5 pt-4 pb-8">
      <div class="max-w-240">
        <VBanner v-if="store.failure.value" tone="danger">
          {{ describeFailure(store.failure.value) }}
        </VBanner>

        <!-- The one place the value ever appears. -->
        <section
          v-if="store.minted.value"
          class="minted mt-4 rounded-control border border-warn-line bg-warn-bg p-4"
          aria-live="polite"
        >
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-warn uppercase">
            Copy it now
          </h2>
          <p class="mx-0 mt-0 mb-3 text-md leading-normal">
            This is the only time <strong>{{ store.minted.value.name }}</strong> will be shown.
            Avior keeps a hash and cannot show it again; if it is lost, revoke it and make another.
          </p>
          <div
            class="secret flex items-center gap-2 overflow-x-auto rounded-control border border-warn-line bg-surface px-3 py-2"
          >
            <code class="flex-1 font-mono text-sm break-all">{{ store.minted.value.token }}</code>
            <VButton size="sm" @click="copyMinted()">{{ copied ? 'Copied' : 'Copy' }}</VButton>
          </div>
          <div class="mt-2 text-right">
            <VButton size="sm" variant="bare" @click="store.dismissMinted()">
              I have copied it
            </VButton>
          </div>
        </section>

        <section class="mt-4 rounded-control border border-line bg-surface p-4">
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            New token
          </h2>
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="mint()">
            <label class="flex min-w-56 flex-1 flex-col gap-1">
              <span class="text-xs font-semibold tracking-label text-fg-soft uppercase">Name</span>
              <VInput
                v-model="name"
                type="text"
                maxlength="80"
                placeholder="laptop agent"
                autocomplete="off"
              />
            </label>
            <label class="flex min-w-0 flex-none flex-col gap-1">
              <span class="text-xs font-semibold tracking-label text-fg-soft uppercase"
                >Expires in</span
              >
              <span class="flex items-center gap-1.5">
                <span class="w-20">
                  <VInput v-model.number="days" class="block" type="number" min="1" max="730" />
                </span>
                <span class="text-sm text-fg-soft">days</span>
              </span>
            </label>
            <VButton type="submit" :disabled="!canMint || store.busy.value">Create token</VButton>
          </form>
          <p class="mx-0 mt-3 mb-0 text-sm text-fg-muted">
            Seven days unless you say otherwise; two years (730) is the most.
          </p>
        </section>

        <section class="mt-4 rounded-control border border-line bg-surface p-4">
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            Your tokens
          </h2>
          <VSkeleton
            v-if="!store.loaded.value && store.failure.value === null"
            variant="table"
            table="inventory"
            :headings="['Name', 'Created', 'Expires', 'Last used', 'State', '']"
            :columns="['w-21/25', 'w-1/2', 'w-1/2', 'w-1/2', 'w-1/3', 'w-1/3']"
            :rows="3"
            label="Reading your tokens"
          />
          <p
            v-else-if="store.loaded.value && store.tokens.value.length === 0"
            class="mx-0 mt-3 mb-0 text-sm text-fg-muted"
          >
            None yet.
          </p>
          <VTable v-else-if="store.tokens.value.length > 0" variant="inventory">
            <template #head>
              <th>Name</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Last used</th>
              <th>State</th>
              <th></th>
            </template>
            <tr
              v-for="token in store.tokens.value"
              :key="token.id"
              :class="[state(token), { 'text-fg-muted': state(token) !== 'live' }]"
            >
              <td class="font-semibold">{{ token.name }}</td>
              <td :title="titled(token.createdAt)">{{ shown(token.createdAt) }}</td>
              <td :title="titled(token.expiresAt)">{{ shown(token.expiresAt) }}</td>
              <td :title="titled(token.lastUsedAt)">{{ shown(token.lastUsedAt) }}</td>
              <td>
                <span
                  class="rounded-pill border px-2 py-0.25 text-2xs capitalize"
                  :class="badgeTone[state(token)]"
                  >{{ state(token) }}</span
                >
              </td>
              <td class="text-right">
                <VButton
                  v-if="state(token) === 'live'"
                  size="sm"
                  variant="bare"
                  :disabled="store.busy.value"
                  @click="store.revoke(token.id)"
                >
                  Revoke
                </VButton>
              </td>
            </tr>
          </VTable>
        </section>
      </div>
    </div>
  </div>
</template>
