import { defineStore } from 'pinia'
import { ref } from 'vue'

import {
  api,
  classify,
  unreachable,
  type AuthMethods,
  type CurrentUser,
  type Failure,
} from '@/shared/api'

export type AuthMethodsStatus = 'idle' | 'loading' | 'ready' | 'error'

export const useSessionStore = defineStore('session', () => {
  const caller = ref<CurrentUser | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const authMethods = ref<AuthMethods | null>(null)
  const authMethodsStatus = ref<AuthMethodsStatus>('idle')
  const authMethodsFailure = ref<Failure | null>(null)

  async function loadAuthMethods(): Promise<void> {
    authMethodsStatus.value = 'loading'
    authMethodsFailure.value = null
    try {
      const { data, error, response } = await api.GET('/auth/methods')
      if (data === undefined) {
        authMethods.value = null
        authMethodsFailure.value = classify(response.status, error)
        authMethodsStatus.value = 'error'
        return
      }
      authMethods.value = data
      authMethodsStatus.value = 'ready'
    } catch (thrown) {
      authMethods.value = null
      authMethodsFailure.value = unreachable(thrown)
      authMethodsStatus.value = 'error'
    }
  }

  async function refresh(): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.GET('/auth/me')
      if (data === undefined) {
        caller.value = null
        // A 401 here is the normal unauthenticated case, not something to show.
        if (response.status !== 401) failure.value = classify(response.status, error)
        return
      }
      caller.value = data
    } catch (thrown) {
      caller.value = null
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  /**
   * signIn sends only the fields the operator filled in.
   *
   * Which of them the deployment needs depends on how it authenticates: the
   * directory wants a password, a local stack wants neither, so sending an
   * empty string would fail the contract's own validation for no reason.
   *
   * No second factor. The directory is the whole of the credential in the
   * deployment this serves, and a field for a code nobody is enrolled in asks
   * every operator to work out that it does not apply to them. The endpoint
   * still accepts one, and an account carrying a secret is still held to it.
   */
  async function signIn(username: string, password: string): Promise<boolean> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.POST('/auth/login', {
        body: {
          username,
          ...(password === '' ? {} : { password }),
        },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      caller.value = data.user
      return true
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      busy.value = false
    }
  }

  async function signOut(): Promise<void> {
    try {
      await api.POST('/auth/logout')
    } catch (thrown) {
      failure.value = unreachable(thrown)
    }
    // The local session is dropped either way: the operator asked to sign out,
    // and leaving them apparently signed in would be worse than a stale row.
    caller.value = null
  }

  return {
    caller,
    failure,
    busy,
    authMethods,
    authMethodsStatus,
    authMethodsFailure,
    loadAuthMethods,
    refresh,
    signIn,
    signOut,
  }
})
