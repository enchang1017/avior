<script setup lang="ts">
import type { RoleDetail } from './useOperators'
import { VBadge, VDialog } from '@/components'

/**
 * What each role opens, for the person about to hand one out.
 *
 * A grant here decides what somebody may do against a production database, and
 * `release_approver` does not say. The permissions are the server's, built from
 * the same table the middleware reads, so this cannot describe a role by what
 * it used to carry.
 */
const props = defineProps<{ open: boolean; roles: RoleDetail[] }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <VDialog :open="props.open" title="What each role opens" @close="emit('close')">
    <p v-if="props.roles.length === 0" class="text-fg-soft">
      The role model could not be read. Reload the page to try again.
    </p>

    <div
      v-for="role in props.roles"
      :key="role.name"
      class="role [&+&]:mt-5 [&+&]:border-t [&+&]:border-line [&+&]:pt-5"
    >
      <div class="flex items-center gap-2">
        <h3 class="m-0 text-md font-semibold">{{ role.label }}</h3>
        <code class="font-mono text-xs text-fg-soft">{{ role.name }}</code>
        <!-- The one an administrator will look for and not find a switch for.
             Saying why here is cheaper than a refusal after the click. -->
        <VBadge v-if="!role.administrable" tone="neutral">not granted here</VBadge>
      </div>

      <p class="mt-1.5 mb-2.5 max-w-xl text-sm">{{ role.summary }}</p>

      <ul class="m-0 grid list-none gap-1.5 p-0">
        <li
          v-for="permission in role.permissions"
          :key="permission.name"
          class="grid grid-cols-[--spacing(48)_1fr] items-baseline gap-3 text-sm max-sm:grid-cols-1 max-sm:gap-0.5"
        >
          <code class="font-mono text-xs text-fg-soft">{{ permission.name }}</code>
          <span>{{ permission.summary }}</span>
        </li>
      </ul>
    </div>
  </VDialog>
</template>
