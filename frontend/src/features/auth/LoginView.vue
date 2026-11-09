<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { describeFailure } from '@/shared/api'
import { forgetAllTabs } from '@/shared/tabStorage'
import { useSessionStore } from '@/stores/session'
import { VButton, VDeepSpace, VHeroStar, VIcon, VInput, VLogo } from '@/components'

const session = useSessionStore()
const router = useRouter()
const route = useRoute()

/*
 * Signing out lands here without a redirect; a session that lapsed lands here
 * with one. Only the first leaves the browser to somebody else, so only then
 * are the statements it held let go. A lapse keeps them for the same person.
 */
onMounted(() => {
  if (route.query['redirect'] === undefined) forgetAllTabs()
})

const username = ref('')
const password = ref('')

async function submit(): Promise<void> {
  if (!(await session.signIn(username.value, password.value))) return
  const redirect = route.query['redirect']
  await router.replace(typeof redirect === 'string' ? redirect : '/')
}
</script>

<template>
  <main
    class="relative grid min-h-screen items-center gap-10 overflow-hidden px-4 py-8 lg:grid-cols-[1.15fr_1fr] lg:px-24"
  >
    <VDeepSpace />

    <section class="hero relative flex flex-col max-lg:hidden">
      <VHeroStar class="mb-4 ml-24" />
      <div class="tagline mt-10 max-w-lg">
        <p class="m-0 hud tracking-widest text-target">Epsilon Carinae · the keel</p>
        <h1 class="mt-2.5 mb-2 text-hero leading-tight font-semibold tracking-tight">
          Find your way through the sea of data.
        </h1>
        <p class="m-0 text-lg text-fg-soft">Every statement guarded, every answer recorded.</p>
      </div>
    </section>

    <form
      class="relative flex w-100 max-w-full flex-col gap-1.25 justify-self-center glow-card px-8 pt-8.5 pb-7.5"
      @submit.prevent="submit"
    >
      <div class="mb-6 flex flex-col gap-2">
        <VLogo wordmark size="md" />
        <small class="text-sm text-fg-soft">Governed SQL gateway</small>
      </div>

      <label for="username" class="eyebrow">Username</label>
      <VInput id="username" v-model="username" type="text" autocomplete="username" required />

      <!-- Only the username is always required. A local stack authenticates on
           it alone; the directory adds a password; the interim store-backed mode
           adds an authenticator code as well. Marking them required here would
           block a sign-in the server would have accepted. -->
      <label for="password" class="mt-3 eyebrow"
        >Password <span class="font-normal text-fg-muted">if required</span></label
      >
      <VInput id="password" v-model="password" type="password" autocomplete="current-password" />

      <p
        v-if="session.failure"
        class="mx-0 mt-3 mb-0 flex items-center gap-1.5 rounded-control border border-danger-line bg-danger-bg px-2 py-1.75 text-xs text-danger"
      >
        <VIcon name="circle-x" />
        {{ describeFailure(session.failure) }}
      </p>

      <VButton variant="primary" class="mt-5 w-full" type="submit" :disabled="session.busy">
        <VIcon v-if="session.busy" name="loader" spin />
        Sign in
      </VButton>
    </form>
  </main>
</template>
