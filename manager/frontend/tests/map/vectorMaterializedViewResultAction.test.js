import { describe, expect, it } from 'vitest'
import {
  vectorMaterializedViewResultAction,
  STALE_SOURCE_FACTS_CHANGED_REASON
} from '../../src/utils/vectorMaterializedViewResultAction'

describe('vectorMaterializedViewResultAction', () => {
  it('does not show refresh action for non-stale results', () => {
    expect(vectorMaterializedViewResultAction({
      status: 'ready',
      task_id: 10
    })).toEqual({
      visible: false,
      canRerun: false,
      labelKey: ''
    })
  })

  it('reruns stale result when the original task is still usable', () => {
    expect(vectorMaterializedViewResultAction({
      status: 'stale',
      task_id: 10,
      error_message: 'vector materialized view target geometry GiST index is missing'
    })).toEqual({
      visible: true,
      canRerun: true,
      labelKey: 'manager.vectorMaterializedView.refreshOptimization'
    })
  })

  it('creates a new task when source facts changed', () => {
    expect(vectorMaterializedViewResultAction({
      status: 'stale',
      task_id: 10,
      error_message: STALE_SOURCE_FACTS_CHANGED_REASON
    })).toEqual({
      visible: true,
      canRerun: false,
      labelKey: 'manager.vectorMaterializedView.recreateOptimizationTask'
    })
  })

  it('creates a new task when stale result has no source task', () => {
    expect(vectorMaterializedViewResultAction({
      status: 'stale'
    })).toEqual({
      visible: true,
      canRerun: false,
      labelKey: 'manager.vectorMaterializedView.recreateOptimizationTask'
    })
  })
})
