import { describe, expect, it } from 'vitest'
import { snapshotUnsavedState } from '../src/composables/useUnsavedChanges'

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
})
