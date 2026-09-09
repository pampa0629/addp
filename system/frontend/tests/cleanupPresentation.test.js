import { describe, expect, it } from 'vitest'

import {
  formatCleanupStateChanges,
  selectCleanupHistoryResults
} from '../src/utils/cleanupPresentation'

const labels = {
  missing: '缺源',
  outdated: '过期',
  disabled: '停用任务',
  deleted: '删除任务'
}

function translate(key, { count }) {
  return `${labels[key.split('.').at(-1)]} ${count}`
}

describe('formatCleanupStateChanges', () => {
  it('明确展示物理删除的任务定义数量', () => {
    expect(formatCleanupStateChanges({ deleted_task_definitions: 3 }, translate)).toBe('删除任务 3')
  })

  it('按统一顺序组合逻辑与物理处理结果', () => {
    expect(formatCleanupStateChanges({
      marked_missing_source: 2,
      disabled_task_definitions: 1,
      deleted_task_definitions: 4
    }, translate)).toBe('缺源 2 / 停用任务 1 / 删除任务 4')
  })

  it('没有状态变化时显示占位符', () => {
    expect(formatCleanupStateChanges({}, translate)).toBe('-')
  })
})

describe('selectCleanupHistoryResults', () => {
  it('从按时间倒序的历史中恢复最近结果和最近可执行评估', () => {
    expect(selectCleanupHistoryResults([
      { task_id: 'execute-2', action: 'execute', status: 'completed' },
      { task_id: 'scan-running', action: 'scan', status: 'running' },
      { task_id: 'scan-1', action: 'scan', status: 'completed_with_errors' }
    ])).toEqual({
      latestScanTaskId: 'scan-running',
      latestExecuteTaskId: 'execute-2',
      latestCompletedScanTaskId: 'scan-1',
      latestCompletedResultTaskId: 'execute-2'
    })
  })

  it('没有已完成任务时不恢复结果', () => {
    expect(selectCleanupHistoryResults([
      { task_id: 'scan-failed', action: 'scan', status: 'failed' }
    ])).toEqual({
      latestScanTaskId: 'scan-failed',
      latestExecuteTaskId: '',
      latestCompletedScanTaskId: '',
      latestCompletedResultTaskId: ''
    })
  })
})
