import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type * as sharedApi from '@/shared/api'

const get = vi.fn()
const post = vi.fn()
vi.mock('@/shared/api', async (importOriginal) => {
  const actual = await importOriginal<typeof sharedApi>()
  return { ...actual, api: { GET: get, POST: post } }
})

const { default: LoginView } = await import('./LoginView.vue')

async function mountLogin(path: string, flush = true) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginView },
      { path: '/', name: 'home', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(LoginView, {
    global: { plugins: [pinia, router] },
  })
  if (flush) await flushPromises()
  return wrapper
}

describe('LoginView', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('shows a loading indicator while auth methods are pending', async () => {
    let resolveMethods!: (value: unknown) => void
    get.mockReturnValue(
      new Promise((resolve) => {
        resolveMethods = resolve
      }),
    )

    const wrapper = await mountLogin('/login', false)

    expect(wrapper.text()).toContain('Checking sign-in methods')
    expect(wrapper.find('#username').exists()).toBe(false)

    resolveMethods({
      data: { mode: 'google', password: true, google: true },
      response: { status: 200 },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Sign in with Google Workspace')
  })

  it('shows a retryable error on transport failure instead of falling back to password mode', async () => {
    get.mockRejectedValueOnce(new Error('network down'))

    const wrapper = await mountLogin('/login')

    expect(wrapper.text()).toContain('Could not reach the Avior service')
    expect(wrapper.find('#username').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Sign in with Google Workspace')

    get.mockResolvedValueOnce({
      data: { mode: 'google', password: true, google: true },
      response: { status: 200 },
    })
    const retry = wrapper.findAll('button').find((b) => b.text().includes('Retry'))
    expect(retry).toBeDefined()
    await retry?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Sign in with Google Workspace')
  })

  it('shows a retryable error on non-2xx auth methods responses', async () => {
    get.mockResolvedValueOnce({
      data: undefined,
      error: { code: 'internal', message: 'Gateway unavailable' },
      response: { status: 503 },
    })

    const wrapper = await mountLogin('/login')

    expect(wrapper.text()).toContain('Gateway unavailable')
    expect(wrapper.find('#username').exists()).toBe(false)
    expect(wrapper.findAll('button').some((b) => b.text().includes('Retry'))).toBe(true)
  })

  it('shows only the password form when Google Workspace auth is off', async () => {
    get.mockResolvedValue({
      data: { mode: 'ldap', password: true, google: false },
      response: { status: 200 },
    })

    const wrapper = await mountLogin('/login')

    expect(wrapper.find('#username').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Sign in with Google Workspace')
  })

  it('shows Google Workspace sign-in and a toggleable local admin form in google mode', async () => {
    get.mockResolvedValue({
      data: { mode: 'google', password: true, google: true },
      response: { status: 200 },
    })

    const wrapper = await mountLogin('/login?redirect=%2Fwrites&error=google_cancelled')

    expect(wrapper.text()).toContain('Sign in with Google Workspace')
    expect(wrapper.text()).toContain('Google Workspace sign-in was cancelled.')
    expect(wrapper.find('#username').exists()).toBe(false)

    const toggle = wrapper.findAll('button').find((b) => b.text().includes('Local administrator'))
    expect(toggle).toBeDefined()
    await toggle?.trigger('click')
    expect(wrapper.find('#username').exists()).toBe(true)
  })

  it('shows a generic refusal banner on google_failed callback error', async () => {
    get.mockResolvedValue({
      data: { mode: 'google', password: true, google: true },
      response: { status: 200 },
    })

    const wrapper = await mountLogin('/login?error=google_failed')

    expect(wrapper.text()).toContain('Google Workspace sign-in was not accepted.')
  })
})
