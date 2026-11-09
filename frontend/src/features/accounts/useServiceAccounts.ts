import { ref } from 'vue'

import { api, classify, unreachable, type Failure } from '@/shared/api'
import type { components } from '@/gen/api'

export type ServiceAccount = components['schemas']['ServiceAccount']
export type ApiToken = components['schemas']['ApiToken']
export type CreatedToken = components['schemas']['CreatedToken']

/** A minted token, and which account it was minted for. */
export type MintedFor = { account: string; token: CreatedToken }

/**
 * Service accounts. FEAT-063.
 *
 * The rule FEAT-053 gave the token page holds here too: a token's value exists
 * in the response that created it and nowhere after. `minted` holds that one
 * value apart from the list, and dismissing it is final.
 *
 * Roles are not granted here. A service account is administered on the account
 * page like anybody's, under the same rules, so this page lists what it holds
 * and sends the reader there to change it.
 */
export function useServiceAccounts() {
  const accounts = ref<ServiceAccount[]>([])
  const minted = ref<MintedFor | null>(null)
  const failure = ref<Failure | null>(null)
  const busy = ref(false)
  const loaded = ref(false)

  async function attempt(work: () => Promise<boolean>): Promise<boolean> {
    busy.value = true
    failure.value = null
    try {
      return await work()
    } catch (thrown) {
      failure.value = unreachable(thrown)
      return false
    } finally {
      busy.value = false
    }
  }

  async function fetchList(): Promise<boolean> {
    const { data, error, response } = await api.GET('/service-accounts')
    if (data === undefined) {
      failure.value = classify(response.status, error)
      return false
    }
    accounts.value = data
    loaded.value = true
    return true
  }

  function load(): Promise<boolean> {
    return attempt(fetchList)
  }

  function create(name: string, description: string): Promise<boolean> {
    return attempt(async () => {
      const { data, error, response } = await api.POST('/service-accounts', {
        body: { name, description },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      return fetchList()
    })
  }

  function mint(account: ServiceAccount, name: string, expiresInDays: number): Promise<boolean> {
    return attempt(async () => {
      const { data, error, response } = await api.POST('/service-accounts/{operatorId}/tokens', {
        params: { path: { operatorId: account.id } },
        body: { name, expiresInDays },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      minted.value = { account: account.username, token: data }
      return fetchList()
    })
  }

  function revoke(account: ServiceAccount, token: ApiToken): Promise<boolean> {
    return attempt(async () => {
      const { error, response } = await api.DELETE(
        '/service-accounts/{operatorId}/tokens/{tokenId}',
        { params: { path: { operatorId: account.id, tokenId: token.id } } },
      )
      if (!response.ok) {
        failure.value = classify(response.status, error)
        return false
      }
      return fetchList()
    })
  }

  /**
   * setDisabled goes through the account page's own endpoint: switching a
   * service account off is the same act, under the same rule and the same
   * record, as switching anybody's off.
   */
  function setDisabled(account: ServiceAccount, disabled: boolean): Promise<boolean> {
    return attempt(async () => {
      const { data, error, response } = await api.PUT('/operators/{operatorId}/disabled', {
        params: { path: { operatorId: account.id } },
        body: { disabled },
      })
      if (data === undefined) {
        failure.value = classify(response.status, error)
        return false
      }
      return fetchList()
    })
  }

  function dismissMinted(): void {
    minted.value = null
  }

  return {
    accounts,
    minted,
    failure,
    busy,
    loaded,
    load,
    create,
    mint,
    revoke,
    setDisabled,
    dismissMinted,
  }
}

/** Whether a token still opens anything, by the rules the server applies. */
export function tokenState(token: ApiToken, now = Date.now()): 'revoked' | 'expired' | 'live' {
  if (token.revokedAt !== undefined && token.revokedAt !== null) return 'revoked'
  if (new Date(token.expiresAt).getTime() <= now) return 'expired'
  return 'live'
}

/**
 * How many live tokens a service account may hold. FEAT-063 AC-5.
 *
 * Mirrored so the page can say so before the click; the server refuses a third
 * whatever this says.
 */
export const maxLiveTokens = 2

export function liveTokens(account: ServiceAccount, now = Date.now()): ApiToken[] {
  return account.tokens.filter((token) => tokenState(token, now) === 'live')
}
