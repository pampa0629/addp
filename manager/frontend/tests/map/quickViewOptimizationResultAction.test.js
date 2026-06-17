import { describe, expect, it } from 'vitest'
import {
  quickViewOptimizationResultAction,
  STALE_SOURCE_FACTS_CHANGED_REASON
} from '../../src/utils/quickViewOptimizationResultAction'

describe('quickViewOptimizationResultAction', () => {
  it('does not show refresh action for non-stale results', () => {
    expect(quickViewOptimizationResultAction({
      status: 'ready',
      task_id: 10
    })).toEqual({
      visible: false,
      canRerun: false,
      labelKey: ''
    })
  })

  it('reruns stale result when the original task is still usable', () => {
    expect(quickViewOptimizationResultAction({
      status: 'stale',
      task_id: 10,
      error_message: 'quick view optimization target geometry GiST index is missing'
    })).toEqual({
      visible: true,
      canRerun: true,
      labelKey: 'manager.quickViewOptimization.refreshOptimization'
    })
  })

  it('creates a new task when source facts changed', () => {
    expect(quickViewOptimizationResultAction({
      status: 'stale',
      task_id: 10,
      error_message: STALE_SOURCE_FACTS_CHANGED_REASON
    })).toEqual({
      visible: true,
      canRerun: false,
      labelKey: 'manager.quickViewOptimization.recreateOptimizationTask'
    })
  })

  it('creates a new task when stale result has no source task', () => {
    expect(quickViewOptimizationResultAction({
      status: 'stale'
    })).toEqual({
      visible: true,
      canRerun: false,
      labelKey: 'manager.quickViewOptimization.recreateOptimizationTask'
    })
  })
})
