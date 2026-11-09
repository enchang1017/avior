<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'

import { VBanner, VButton, VInput, VPageHeader, VTabs } from '@/components'
import { describeFailure } from '@/shared/api'
import { useTokens } from './useTokens'

/**
 * Connect an agent to Avior's MCP endpoint. FEAT-060.
 *
 * Two steps, the way every MCP provider's page does it: get a token, paste one
 * line into your client. The token is minted here rather than on the tokens
 * page so the line can be shown complete, once, ready to copy — a placeholder
 * the reader has to fill in by hand is the step people get wrong.
 */
const tokens = useTokens()
const name = ref('agent')
const client = ref<'claude' | 'codex' | 'other'>('claude')

const clients = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
  { id: 'other', label: 'Cursor & others' },
] as const
const copied = ref('')

const endpoint = computed(() => `${window.location.origin}/api/v1/mcp`)
const token = computed(() => tokens.minted.value?.token ?? '<YOUR_TOKEN>')
const canMint = computed(() => name.value.trim() !== '' && !tokens.busy.value)

const claudeCommand = computed(
  () =>
    `claude mcp add --transport http avior ${endpoint.value} --header "Authorization: Bearer ${token.value}"`,
)
const codexConfig = computed(
  () => `[mcp_servers.avior]\nurl = "${endpoint.value}"\nbearer_token_env_var = "AVIOR_TOKEN"`,
)
const codexExport = computed(() => `export AVIOR_TOKEN=${token.value}`)
const otherConfig = computed(() =>
  JSON.stringify(
    {
      mcpServers: {
        avior: { url: endpoint.value, headers: { Authorization: `Bearer ${token.value}` } },
      },
    },
    null,
    2,
  ),
)

async function mint(): Promise<void> {
  await tokens.mint(name.value.trim(), 7)
}

async function copy(what: string, text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = what
    window.setTimeout(() => {
      if (copied.value === what) copied.value = ''
    }, 2000)
  } catch {
    // Refused on an insecure origin. The text stays selectable; the label says so instead of pretending.
    copied.value = 'refused'
  }
}

function label(what: string): string {
  if (copied.value === 'refused') return 'Select and copy'
  return copied.value === what ? 'Copied' : 'Copy'
}
</script>

<template>
  <div class="list-page">
    <VPageHeader
      icon="zap"
      title="Connect your agent"
      subtitle="Guarded, recorded, as you. Two steps."
      :busy="tokens.busy.value"
    />

    <div class="min-h-0 flex-1 overflow-auto px-5 pt-4 pb-8">
      <div class="max-w-208">
        <VBanner v-if="tokens.failure.value" tone="danger">
          {{ describeFailure(tokens.failure.value) }}
        </VBanner>

        <section class="mt-4 rounded-control border border-line bg-surface px-5 pt-4 pb-5">
          <h2 class="mx-0 mt-0 mb-3.5 flex items-center gap-2.5 text-lg">
            <span
              class="grid size-6 place-items-center rounded-pill bg-primary text-xs font-bold text-on-primary"
              >1</span
            >
            Create a token
          </h2>

          <div v-if="tokens.minted.value" class="minted flex flex-wrap items-center gap-2">
            <code
              class="secret min-w-0 flex-1 rounded-control border border-warn-line bg-warn-bg px-3 py-2 font-mono text-sm break-all"
              >{{ tokens.minted.value.token }}</code
            >
            <VButton size="sm" @click="copy('token', tokens.minted.value.token)">
              {{ label('token') }}
            </VButton>
            <p class="m-0 basis-full text-sm text-fg-muted">
              Shown once. It is already filled into step 2.
            </p>
          </div>

          <form v-else class="flex flex-wrap items-center gap-2" @submit.prevent="mint()">
            <VInput
              v-model="name"
              class="min-w-56 flex-1"
              type="text"
              maxlength="80"
              placeholder="Name, e.g. laptop agent"
              autocomplete="off"
              aria-label="Token name"
            />
            <VButton
              type="submit"
              variant="primary"
              :disabled="!canMint"
              :aria-busy="tokens.busy.value ? 'true' : undefined"
            >
              Create token
            </VButton>
            <p class="mx-0 mt-1 mb-2 basis-full text-sm text-fg-muted">
              Read-only, 7 days. Have one already?
              <RouterLink :to="{ name: 'tokens' }" class="text-primary">Manage tokens</RouterLink>
            </p>
          </form>
        </section>

        <section class="mt-4 rounded-control border border-line bg-surface px-5 pt-4 pb-5">
          <h2 class="mx-0 mt-0 mb-3.5 flex items-center gap-2.5 text-lg">
            <span
              class="grid size-6 place-items-center rounded-pill bg-primary text-xs font-bold text-on-primary"
              >2</span
            >
            Add Avior to your client
          </h2>

          <VTabs
            :model-value="client"
            :tabs="clients"
            label="Client"
            @update:model-value="client = $event as typeof client"
          />

          <template v-if="client === 'claude'">
            <p class="mx-0 mt-1 mb-2 text-sm text-fg-muted">Run once in a terminal.</p>
            <div
              class="command mb-2 flex items-start gap-2 rounded-control border border-line-strong bg-surface-sunken px-3 py-2.5"
            >
              <pre
                class="m-0 min-w-0 flex-1 font-mono text-sm leading-normal break-all whitespace-pre-wrap"
              ><code>{{ claudeCommand }}</code></pre>
              <VButton size="sm" @click="copy('claude', claudeCommand)">
                {{ label('claude') }}
              </VButton>
            </div>
          </template>

          <template v-else-if="client === 'codex'">
            <p class="mx-0 mt-1 mb-2 text-sm text-fg-muted">
              Add to <code class="font-mono text-fg">~/.codex/config.toml</code>:
            </p>
            <div
              class="command mb-2 flex items-start gap-2 rounded-control border border-line-strong bg-surface-sunken px-3 py-2.5"
            >
              <pre
                class="m-0 min-w-0 flex-1 font-mono text-sm leading-normal break-all whitespace-pre-wrap"
              ><code>{{ codexConfig }}</code></pre>
              <VButton size="sm" @click="copy('codex', codexConfig)">{{ label('codex') }}</VButton>
            </div>
            <p class="mx-0 mt-1 mb-2 text-sm text-fg-muted">And in your shell profile:</p>
            <div
              class="command mb-2 flex items-start gap-2 rounded-control border border-line-strong bg-surface-sunken px-3 py-2.5"
            >
              <pre
                class="m-0 min-w-0 flex-1 font-mono text-sm leading-normal break-all whitespace-pre-wrap"
              ><code>{{ codexExport }}</code></pre>
              <VButton size="sm" variant="bare" @click="copy('export', codexExport)">
                {{ label('export') }}
              </VButton>
            </div>
          </template>

          <template v-else>
            <p class="mx-0 mt-1 mb-2 text-sm text-fg-muted">
              Add to your client's MCP configuration:
            </p>
            <div
              class="command mb-2 flex items-start gap-2 rounded-control border border-line-strong bg-surface-sunken px-3 py-2.5"
            >
              <pre
                class="m-0 min-w-0 flex-1 font-mono text-sm leading-normal break-all whitespace-pre-wrap"
              ><code>{{ otherConfig }}</code></pre>
              <VButton size="sm" @click="copy('other', otherConfig)">{{ label('other') }}</VButton>
            </div>
          </template>
        </section>

        <p class="mt-5 text-md text-fg-soft">
          Then ask it: <em>“which databases can I see?”</em> — it lists what your roles reach, and
          nothing else. Writes are not offered; those are done here, signed in.
        </p>
      </div>
    </div>
  </div>
</template>
