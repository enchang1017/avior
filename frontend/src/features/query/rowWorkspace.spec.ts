import { beforeEach, expect, it, vi } from 'vitest'
import { createRowWorkspace, resetRowWorkspace } from './rowWorkspace'
import { useRowEdits } from './useRowEdits'
import { createPinia, setActivePinia } from 'pinia'
import { useSessionStore } from '@/stores/session'
import type * as sharedApi from '@/shared/api'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/shared/api', async (original) => ({
  ...(await original<typeof sharedApi>()),
  api: { POST: post },
}))
beforeEach(() => {
  resetRowWorkspace()
  setActivePinia(createPinia())
  post.mockReset()
})
it('retains drafts on route remount but invalidates the old model on owner change', () => {
  const first = createRowWorkspace('user', 'instance')
  first.state.value = useRowEdits()
  const reset = vi.spyOn(first.state.value, 'reset')
  expect(createRowWorkspace('user', 'instance').state).toBe(first.state)
  expect(createRowWorkspace('other', 'instance').state.value).toBeNull()
  expect(reset).toHaveBeenCalledOnce()
  expect(first.state.value).toBeNull()
})
it('clears cached submitted snapshots when signing out outside Query, even before logout returns', async () => {
  const session = useSessionStore()
  const first = createRowWorkspace('user', 'instance')
  first.state.value = useRowEdits()
  const reset = vi.spyOn(first.state.value, 'reset')
  let resolve!: (value: unknown) => void
  post.mockReturnValueOnce(
    new Promise((done) => {
      resolve = done
    }),
  )
  const signingOut = session.signOut()
  expect(reset).toHaveBeenCalledOnce()
  expect(first.state.value).toBeNull()
  expect(createRowWorkspace('user', 'instance').state.value).toBeNull()
  resolve({})
  await signingOut
})
