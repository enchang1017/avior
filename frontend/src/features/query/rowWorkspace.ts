import { inject, shallowRef, ref, type Ref, type ShallowRef, type InjectionKey } from 'vue'
import type { RowDraft, useRowEdits } from './useRowEdits'
export type RowEdits = ReturnType<typeof useRowEdits>
export type RowBridge = {
  state: ShallowRef<RowEdits | null>
  open: Ref<boolean>
  focus: ShallowRef<RowDraft | null>
  refresh: Ref<number>
  instanceId: string
  editing: Ref<boolean>
  flush: (() => boolean) | null
  navigate: ((row: RowDraft) => Promise<void>) | null
}
export const rowWorkspaceKey: InjectionKey<RowBridge> = Symbol('row-workspace')
let owner = ''
let cached: RowBridge['state'] | null = null
export function resetRowWorkspace(): void {
  cached?.value?.reset()
  if (cached) cached.value = null
  cached = null
  owner = ''
}
export function createRowWorkspace(userId: string, instanceId: string): RowBridge {
  if (owner !== userId + instanceId) {
    resetRowWorkspace()
    owner = userId + instanceId
  }
  cached ??= shallowRef<RowEdits | null>(null)
  return {
    state: cached,
    open: ref(false),
    focus: shallowRef<RowDraft | null>(null),
    refresh: ref(0),
    instanceId,
    editing: ref(false),
    flush: null,
    navigate: null,
  }
}
export function rowWorkspace(): RowBridge {
  return inject(rowWorkspaceKey) ?? createRowWorkspace('', '')
}
