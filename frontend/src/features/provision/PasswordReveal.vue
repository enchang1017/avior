<script setup lang="ts">
import { ref } from 'vue'

import { VButton } from '@/components'
import type { ProvisionedLogin } from './useRoles'

/**
 * The one place a login's password ever appears. FEAT-061.
 *
 * It lives inside a dialog, and Done is the only way out: when the dialog
 * closes the value is dropped, because the server cannot show it again and a
 * page that kept it around would be a page that could leak it.
 */
defineProps<{ login: ProvisionedLogin }>()
const emit = defineEmits<{ done: [] }>()

const copied = ref(false)

const line = 'm-0 mb-3 text-md leading-normal'

async function copy(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value)
    copied.value = true
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <div aria-live="polite">
    <p :class="line">
      Password for <strong>{{ login.name }}</strong> — shown once. Put it in OpenBao now; Avior
      keeps no copy, and closing this is the end of it.
    </p>
    <div
      class="secret flex items-center gap-2 rounded-control border border-warn-line bg-warn-bg px-3 py-2.5"
    >
      <code class="flex-1 font-mono text-sm break-all">{{ login.password }}</code>
      <VButton size="sm" @click="copy(login.password)">{{ copied ? 'Copied' : 'Copy' }}</VButton>
    </div>
    <p v-if="login.memberOf.length > 0" :class="[line, 'text-fg-muted']">
      Member of
      <code v-for="g in login.memberOf" :key="g" class="ml-1.5 font-mono text-fg">{{ g }}</code>
    </p>
    <div class="mt-4 flex justify-end">
      <VButton variant="primary" @click="emit('done')">Done — I have stored it</VButton>
    </div>
  </div>
</template>
