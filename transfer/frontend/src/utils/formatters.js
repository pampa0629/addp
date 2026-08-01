import { describeCron } from '@common-ui'
import { executionStatusLabelKey, executionStatusTagType } from './executionStatus.mjs'

// Transfer 模块特定的格式化函数
export const formatDuration = (ms) => {
  if (!ms || ms < 0) return '-'

  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)

  if (hours > 0) {
    return `${hours}小时${minutes % 60}分钟`
  }
  if (minutes > 0) {
    return `${minutes}分钟${seconds % 60}秒`
  }
  return `${seconds}秒`
}

export const formatProgress = (progress) => {
  if (progress == null || progress < 0) return '0%'
  return `${Math.min(100, progress).toFixed(1)}%`
}

export const formatSchedule = (cron) => {
  if (!cron) return '手动执行'
  return describeCron(cron)
}

export const getTaskTypeLabel = (type) => {
  const labels = {
    sync: '同步'
  }
  return labels[type] || type
}

// 任务状态标签（简化版）
export const getTaskStatusLabel = (task) => {
  if (!task) return '未知'

  if (task?.config?.runtime?.boundary === 'continuous') {
    if (task.desired_state === 'paused') return '已暂停'
    if (task.desired_state === 'stopped') return '已停止'
    return '执行中'
  }

  // 手动任务（无 schedule）
  if (!task.schedule) {
    return task.status === 'running' ? '执行中' : '空闲'
  }

  // 定时任务（有 schedule）
  if (task.status === 'running') {
    return '执行中'
  }
  return task.enabled ? '已启动' : '未启动'
}

export const getTaskStatusTagType = (task) => {
	if (task?.status === 'blocked') return 'danger'
	const label = getTaskStatusLabel(task)
  const types = {
    执行中: 'primary',
    空闲: 'info',
    已启动: 'success',
    未启动: 'info',
    已暂停: 'warning',
    已停止: 'info',
    未知: 'info'
  }
  return types[label] || 'info'
}

export const getExecutionTagType = executionStatusTagType

export const getExecutionLabel = (status, t) => {
  const key = executionStatusLabelKey(status)
  return key && typeof t === 'function' ? t(key) : (status || 'pending')
}
