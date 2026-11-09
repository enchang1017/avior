<script setup lang="ts">
import { RouterLink, useRouter } from 'vue-router'

import { useSessionStore } from '@/stores/session'
import { VBadge, VButton, VIcon } from '@/components'

/**
 * What the account menu holds: who is signed in, their pages and the way out.
 * A chunk of its own, so the shell every page loads first does not carry it.
 */
const session = useSessionStore()
const router = useRouter()

const accountLinks = [
  { route: 'profile', icon: 'user', label: 'Account' },
  { route: 'tokens', icon: 'key', label: 'API tokens' },
  { route: 'agent-setup', icon: 'zap', label: 'Connect your agent' },
] as const

/*
 * Signing out has to navigate, not only forget: the guard that sends somebody
 * to the login page only runs on a navigation.
 */
async function signOut(): Promise<void> {
  await session.signOut()
  await router.push({ name: 'login' })
}
</script>

<template>
  <div class="border-b border-line px-2.5 py-2">
    <p class="m-0 font-semibold">{{ session.caller?.username ?? '' }}</p>
    <p class="m-0 mt-1.5 flex flex-wrap gap-1">
      <VBadge v-for="role in session.caller?.roles ?? []" :key="role" size="sm">
        {{ role }}
      </VBadge>
      <span v-if="(session.caller?.roles ?? []).length === 0" class="text-xs text-fg-muted">
        No roles yet
      </span>
    </p>
  </div>

  <RouterLink
    v-for="link in accountLinks"
    :key="link.route"
    class="flex items-center gap-2 rounded-control px-2.5 py-2 text-sm text-fg no-underline hover:bg-surface-sunken"
    :to="{ name: link.route }"
  >
    <VIcon :name="link.icon" class="text-fg-muted" />
    {{ link.label }}
  </RouterLink>

  <div class="flex items-center border-t border-line px-2.5 py-1.5">
    <VButton size="sm" icon="log-out" @click="signOut()">Sign out</VButton>
  </div>
</template>
