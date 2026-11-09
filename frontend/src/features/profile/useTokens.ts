import { ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type ApiToken = components['schemas']['ApiToken']
export type CreatedToken = components['schemas']['CreatedToken']

/**
 * The caller's API tokens. FEAT-053.
 *
 * One rule shapes this composable: the value of a token exists in exactly one
 * place, the response that created it, and nowhere afterwards. So `minted`
 * holds the one just made, separately from the list, and is cleared the moment
 * the operator dismisses it — the list never carries a value and the server
 * cannot give one back.
 */
export function useTokens() {
  const tokens = ref<ApiToken[]>([])
  const minted = ref<CreatedToken | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const loaded = ref(false)

  async function load(): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.GET('/tokens')
      if (data === undefined) {
        tokens.value = []
        failure.value = classify(response.status, error)
        return
      }
      tokens.value = data
      loaded.value = true
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  async function mint(name: string, expiresInDays: number): Promise<boolean> {
    busy.value = true
    failure.value = null
    try {
      const { data, error, response } = await api.POST('/tokens', {
        body: { name, expiresInDays },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      minted.value = data
      await load()
      return true
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      busy.value = false
    }
  }

  async function revoke(id: string): Promise<void> {
    busy.value = true
    failure.value = null
    try {
      const { error, response } = await api.DELETE('/tokens/{tokenId}', {
        params: { path: { tokenId: id } },
      })
      if (!response.ok) {
        failure.value = classify(response.status, error)
        return
      }
      await load()
    } catch (thrown) {
      failure.value = unreachable(thrown)
    } finally {
      busy.value = false
    }
  }

  function dismissMinted(): void {
    minted.value = null
  }

  return { tokens, minted, failure, busy, loaded, load, mint, revoke, dismissMinted }
}
