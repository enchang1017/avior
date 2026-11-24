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

async function mountLogin(path: string) {
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
  await flushPromises()
  return wrapper
}

describe('LoginView', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
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
