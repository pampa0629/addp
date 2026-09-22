import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { readFileSync } from 'node:fs'
import { snapshotUnsavedState, useUnsavedChanges } from '../src/composables/useUnsavedChanges'
import { useUnsavedChangesGuard } from '@common-ui'

vi.mock('@common-ui', () => ({ useUnsavedChangesGuard: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({}) }))

describe('unsaved changes snapshot', () => {
  it('相同编辑状态生成相同快照', () => {
    const state = { name: '领队', tags: ['户外'], element_ids: [41, 42] }

    expect(snapshotUnsavedState(state)).toBe(snapshotUnsavedState({ ...state }))
  })

  it('字段或关联变化会改变快照', () => {
    const saved = snapshotUnsavedState({ name: '领队', element_ids: [41] })

    expect(snapshotUnsavedState({ name: '领队（更新）', element_ids: [41] })).not.toBe(saved)
    expect(snapshotUnsavedState({ name: '领队', element_ids: [41, 42] })).not.toBe(saved)
  })

  it('分区保存仅更新成功分区的基线', () => {
    const state = reactive({ identity: { tags: [] }, revision: { name: '原名称' } })
    const { isDirty, markSaved } = useUnsavedChanges({ state })
    markSaved()
    state.identity.tags.push('户外')
    state.revision.name = '新名称'
    markSaved('identity')
    expect(isDirty.value).toBe(true)
    markSaved('revision')
    expect(isDirty.value).toBe(false)
    state.identity.tags.push('新标签')
    markSaved('revision')
    expect(isDirty.value).toBe(true)
    expect(useUnsavedChangesGuard.mock.lastCall[0].isDirty()).toBe(true)
  })

  it('Standard 只维护保存基线，不另行实现离页守卫', () => {
    const source = readFileSync(new URL('../src/composables/useUnsavedChanges.js', import.meta.url), 'utf8')
    expect(source).toContain('useUnsavedChangesGuard(')
    expect(source).not.toMatch(/onBeforeRouteLeave|onBeforeRouteUpdate|beforeunload|ElMessageBox|postMessage/)
    for (const view of ['ElementDetail', 'GlossaryDetail']) {
      const content = readFileSync(new URL(`../src/views/${view}.vue`, import.meta.url), 'utf8')
      expect(content).toContain('useUnsavedChanges({ state: editableState })')
    }
  })
})
