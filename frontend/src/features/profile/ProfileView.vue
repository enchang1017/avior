<script setup lang="ts">
import { computed } from 'vue'

import { RouterLink } from 'vue-router'

import { VPageHeader } from '@/components'
import { useSessionStore } from '@/stores/session'

/**
 * Who Avior thinks you are.
 *
 * Every fact here comes from the session rather than from a page of its own,
 * because every one of them is already what the rest of the application decides
 * on: the rail is drawn from these roles, the console refuses a database these
 * grants do not reach, and the banner names this environment. A second answer
 * would be a second thing to be wrong.
 */
const session = useSessionStore()

const caller = computed(() => session.caller)
const roles = computed(() => caller.value?.roles ?? [])
const link = 'text-primary no-underline hover:underline'
const permissions = computed(() => [...(caller.value?.permissions ?? [])].sort())
</script>

<template>
  <div class="list-page">
    <VPageHeader icon="user" title="Who you are here" :subtitle="caller?.username ?? ''" />

    <div class="min-h-0 flex-1 overflow-auto px-5 pt-4 pb-8">
      <div
        v-if="caller"
        class="mt-4 grid grid-cols-[repeat(auto-fit,minmax(--spacing(80),1fr))] gap-4"
      >
        <section class="rounded-control border border-line bg-surface p-4">
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            Identity
          </h2>
          <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
            <dt class="text-sm text-fg-soft">Username</dt>
            <dd class="m-0 font-mono text-sm">{{ caller.username }}</dd>
            <dt class="text-sm text-fg-soft">Deployment</dt>
            <dd class="m-0 font-mono text-sm">{{ caller.environment }}</dd>
            <dt class="text-sm text-fg-soft">Instance</dt>
            <dd class="m-0 font-mono text-sm">
              {{ caller.instance.name }} ({{ caller.instance.id }})
            </dd>
          </dl>
        </section>

        <section class="rounded-control border border-line bg-surface p-4">
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            Roles
          </h2>
          <p v-if="roles.length === 0" class="m-0 text-sm text-fg-muted">
            None yet. An administrator grants these; until one does, you can sign in and reach
            nothing.
          </p>
          <ul v-else class="m-0 flex list-none flex-wrap gap-1.5 p-0">
            <li
              v-for="role in roles"
              :key="role"
              class="rounded-pill border border-line-strong px-2 py-0.5 text-xs"
            >
              {{ role }}
            </li>
          </ul>

          <h2 class="mx-0 mt-5 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            What they open
          </h2>
          <ul
            v-if="permissions.length > 0"
            class="m-0 list-none p-0 font-mono text-xs text-fg-soft"
          >
            <li v-for="permission in permissions" :key="permission" class="py-0.5">
              {{ permission }}
            </li>
          </ul>
          <p v-else class="m-0 text-sm text-fg-muted">Nothing.</p>
        </section>

        <section class="rounded-control border border-line bg-surface p-4">
          <h2 class="mx-0 mt-0 mb-3 text-xs font-bold tracking-wider text-fg-soft uppercase">
            API tokens
          </h2>
          <p class="mx-0 mt-3 mb-0 text-md">
            <RouterLink :to="{ name: 'tokens' }" :class="link">Manage your tokens</RouterLink>
          </p>
        </section>
      </div>
    </div>
  </div>
</template>
