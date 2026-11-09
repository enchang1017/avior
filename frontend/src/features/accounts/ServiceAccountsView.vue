<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import {
  liveTokens,
  maxLiveTokens,
  tokenState,
  useServiceAccounts,
  type ServiceAccount,
} from './useServiceAccounts'
import {
  VBadge,
  VBanner,
  VButton,
  VDialog,
  VEmpty,
  VField,
  VInput,
  VPageHeader,
  VSkeleton,
  VTable,
} from '@/components'
import { describeFailure } from '@/shared/api'
import { fullStamp, stamp } from '@/shared/timestamp'

/**
 * Service accounts. FEAT-063.
 *
 * An account for an agent that is nobody's person. It is made here and given
 * tokens here; its roles are granted on the account page, like anybody's, so
 * this page shows what it holds and does not offer a second way to change it.
 */
const page = useServiceAccounts()

const panel = 'mt-4 rounded-control border border-line bg-surface p-4'
const heading = 'mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase'
const actions = 'mt-2 flex justify-end gap-2'

const stateTone: Record<ReturnType<typeof tokenState>, string> = {
  live: 'border-ok-line bg-ok-bg text-ok',
  expired: 'border-warn-line bg-warn-bg text-warn',
  revoked: 'border-line-strong',
}

const name = ref('svc-')
const description = ref('')
const canCreate = computed(
  () => name.value.trim().length > 4 && description.value.trim() !== '' && !page.busy.value,
)

async function create(): Promise<void> {
  if (await page.create(name.value.trim(), description.value.trim())) {
    name.value = 'svc-'
    description.value = ''
  }
}

/** The account a token is being minted for, while the dialog is open. */
const minting = ref<ServiceAccount | null>(null)
const tokenName = ref('')
const days = ref(30)
const copied = ref(false)
const canMint = computed(
  () => tokenName.value.trim() !== '' && days.value >= 1 && days.value <= 90 && !page.busy.value,
)

function openMint(account: ServiceAccount): void {
  minting.value = account
  tokenName.value = ''
  days.value = 30
  copied.value = false
}

async function mint(): Promise<void> {
  if (minting.value === null) return
  await page.mint(minting.value, tokenName.value.trim(), days.value)
}

/** Closing the dialog drops the value: the server cannot show it again. */
function closeMint(): void {
  minting.value = null
  page.dismissMinted()
}

async function copyMinted(): Promise<void> {
  if (page.minted.value === null) return
  try {
    await navigator.clipboard.writeText(page.minted.value.token.token)
    copied.value = true
  } catch {
    copied.value = false
  }
}

function mintWithheld(account: ServiceAccount): string {
  if (account.disabled) return 'This account is switched off. Switch it on to mint a token.'
  if (liveTokens(account).length >= maxLiveTokens) {
    return 'It already holds two live tokens. Revoke one first.'
  }
  return ''
}

function shown(value: string | null | undefined): string {
  return value === undefined || value === null ? '—' : stamp(value)
}

function titled(value: string | null | undefined): string {
  return value === undefined || value === null ? '' : fullStamp(value)
}

onMounted(() => void page.load())
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="bot"
      title="Service accounts"
      :count="page.accounts.value.length"
      subtitle="An agent that is nobody's person. It never signs in."
      :busy="page.busy.value"
    >
      <VButton variant="bare" icon="users" :to="{ name: 'admin-accounts' }">
        Roles are given on Account management
      </VButton>
    </VPageHeader>

    <div class="min-h-0 flex-1 overflow-auto px-5 pt-4 pb-8">
      <div class="max-w-256">
        <VBanner v-if="page.failure.value" tone="danger">
          {{ describeFailure(page.failure.value) }}
        </VBanner>

        <section :class="panel">
          <h2 :class="heading">New service account</h2>
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="create()">
            <VField label="Name" for="service-name">
              <VInput
                id="service-name"
                v-model="name"
                type="text"
                maxlength="63"
                autocomplete="off"
                spellcheck="false"
              />
            </VField>
            <VField label="What it is for" for="service-description" wide>
              <VInput
                id="service-description"
                v-model="description"
                type="text"
                maxlength="500"
                placeholder="Reporter, answering #data-help. Run by the platform team."
                autocomplete="off"
              />
            </VField>
            <VButton type="submit" variant="primary" :disabled="!canCreate">Create</VButton>
          </form>
        </section>

        <section v-if="!page.loaded.value && page.failure.value === null" :class="panel">
          <VSkeleton
            variant="table"
            table="inventory"
            :headings="['Token', 'Created', 'Expires', 'Last used', 'State', '']"
            :columns="['w-21/25', 'w-1/2', 'w-1/2', 'w-1/2', 'w-1/3', 'w-1/3']"
            :rows="2"
            label="Reading service accounts"
          />
        </section>
        <VEmpty v-else-if="page.loaded.value && page.accounts.value.length === 0">
          No service accounts yet.
        </VEmpty>

        <section v-for="account in page.accounts.value" :key="account.id" :class="panel">
          <header class="flex items-center gap-2">
            <span
              class="font-mono font-semibold"
              :class="{ 'text-fg-soft line-through': account.disabled }"
              >{{ account.username }}</span
            >
            <VBadge v-if="account.disabled" tone="danger">switched off</VBadge>
            <span class="flex-1" />
            <VButton
              size="sm"
              icon="key"
              :disabled="page.busy.value || mintWithheld(account) !== ''"
              :title="mintWithheld(account) || 'Mint a token for this account.'"
              @click="openMint(account)"
            >
              New token
            </VButton>
            <VButton
              size="sm"
              :icon="account.disabled ? 'lock' : 'lock-open'"
              :danger="!account.disabled"
              :disabled="page.busy.value"
              :title="
                account.disabled
                  ? 'Let its tokens work again. They were never revoked.'
                  : 'Refuse every token of it from the next call. The tokens are kept.'
              "
              @click="page.setDisabled(account, !account.disabled)"
            >
              {{ account.disabled ? 'Switch on' : 'Switch off' }}
            </VButton>
          </header>
          <p class="mx-0 mt-2 mb-1 text-md">{{ account.description }}</p>
          <p class="mx-0 mt-0 mb-3 flex flex-wrap items-center gap-1.5 text-sm text-fg-soft">
            Created by <strong>{{ account.createdBy || 'an account since removed' }}</strong>
            {{ shown(account.createdAt) }} · Roles:
            <span v-if="account.roles.length === 0" class="text-fg-soft">none</span>
            <VBadge v-for="role in account.roles" :key="role">{{ role }}</VBadge>
          </p>

          <p v-if="account.tokens.length === 0" class="m-0 text-sm text-fg-soft">No tokens.</p>
          <VTable v-else variant="inventory">
            <template #head>
              <th>Token</th>
              <th>Created</th>
              <th>Expires</th>
              <th>Last used</th>
              <th>State</th>
              <th></th>
            </template>
            <tr
              v-for="token in account.tokens"
              :key="token.id"
              :class="[tokenState(token), { 'text-fg-muted': tokenState(token) !== 'live' }]"
            >
              <td class="font-semibold">{{ token.name }}</td>
              <td :title="titled(token.createdAt)">{{ shown(token.createdAt) }}</td>
              <td :title="titled(token.expiresAt)">{{ shown(token.expiresAt) }}</td>
              <td :title="titled(token.lastUsedAt)">{{ shown(token.lastUsedAt) }}</td>
              <td>
                <span
                  class="rounded-pill border px-2 py-0.25 text-2xs capitalize"
                  :class="stateTone[tokenState(token)]"
                  >{{ tokenState(token) }}</span
                >
              </td>
              <td class="text-right">
                <VButton
                  v-if="tokenState(token) === 'live'"
                  size="sm"
                  variant="bare"
                  :disabled="page.busy.value"
                  @click="page.revoke(account, token)"
                >
                  Revoke
                </VButton>
              </td>
            </tr>
          </VTable>
        </section>

        <VDialog
          :open="minting !== null"
          :title="page.minted.value ? 'Copy it now' : `New token for ${minting?.username ?? ''}`"
          @close="closeMint()"
        >
          <form
            v-if="page.minted.value === null"
            class="flex flex-col gap-3"
            @submit.prevent="mint()"
          >
            <p class="m-0 text-sm leading-normal text-fg-soft">
              The token reads as the service account and nothing else: it cannot write, approve, or
              mint another. Two can be live at once, so one can replace the other without downtime.
            </p>
            <VField label="Name" for="token-name" wide>
              <VInput
                id="token-name"
                v-model="tokenName"
                type="text"
                maxlength="80"
                placeholder="production, rotated 2026-10"
                autocomplete="off"
              />
            </VField>
            <VField label="Expires in days" for="token-days">
              <VInput id="token-days" v-model.number="days" type="number" min="1" max="730" />
            </VField>
            <div :class="actions">
              <VButton variant="bare" @click="closeMint()">Cancel</VButton>
              <VButton type="submit" variant="primary" :disabled="!canMint">Mint token</VButton>
            </div>
          </form>
          <div v-else aria-live="polite">
            <p class="mx-0 mt-0 mb-3 text-md leading-normal">
              <strong>{{ page.minted.value.token.name }}</strong> for
              <strong>{{ page.minted.value.account }}</strong> — shown once. Put it where the agent
              reads its secrets now; Avior keeps a hash and cannot show it again.
            </p>
            <div
              class="secret flex items-center gap-2 rounded-control border border-warn-line bg-warn-bg px-3 py-2.5"
            >
              <code class="flex-1 font-mono text-sm break-all">{{
                page.minted.value.token.token
              }}</code>
              <VButton size="sm" @click="copyMinted()">{{ copied ? 'Copied' : 'Copy' }}</VButton>
            </div>
            <div :class="actions">
              <VButton variant="primary" @click="closeMint()">Done — I have stored it</VButton>
            </div>
          </div>
        </VDialog>
      </div>
    </div>
  </div>
</template>
