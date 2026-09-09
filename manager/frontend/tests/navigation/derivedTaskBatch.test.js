import { describe, expect, it, vi } from 'vitest'

import { deleteSelectedDerivedTasks } from '../../src/utils/derivedTaskBatch'

describe('deleteSelectedDerivedTasks', () => {
  it('只删除用户明确选择的任务并报告结果', async () => {
    const deleteTask = vi.fn()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error('conflict'))

    await expect(deleteSelectedDerivedTasks([
      { id: 7, task_type: 'vector_tile_cache_generation' },
      { id: 9, task_type: 'model_3d_glb_generation' }
    ], deleteTask)).resolves.toEqual({ requested: 2, deleted: 1, failed: 1 })

    expect(deleteTask.mock.calls).toEqual([
      ['vector_tile_cache_generation', 7],
      ['model_3d_glb_generation', 9]
    ])
  })

  it('空选择不会调用删除接口', async () => {
    const deleteTask = vi.fn()
    await expect(deleteSelectedDerivedTasks([], deleteTask)).resolves.toEqual({ requested: 0, deleted: 0, failed: 0 })
    expect(deleteTask).not.toHaveBeenCalled()
  })
})
