// 从 common-frontend 导入统一的格式化工具
export { formatBytes, formatDate, safeStringify } from '@addp/common-frontend/basic'
import { describeCron } from '@addp/common-frontend/basic'

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
    import: '导入',
    export: '导出',
    sync: '同步'
  }
  return labels[type] || type
}

// 任务状态标签（考虑手动任务和定时任务的区别）
export const getTaskStatusLabel = (task) => {
  if (!task) return '未执行'

  // 手动任务（无 schedule）
  if (!task.schedule) {
    const labels = {
      pending: '未执行',
      running: '执行中',
      stopped: '已停止',
      completed: '已完成'
    }
    return labels[task.status] || '未执行'
  }

  // 定时任务（有 schedule）
  if (['scheduled', 'running'].includes(task.status)) return '已启动'
  if (['pending', 'paused'].includes(task.status)) return '未启动'
  if (task.status === 'stopped') return '已停止'
  return '未启动'
}

export const getTaskStatusTagType = (task) => {
  const label = getTaskStatusLabel(task)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    已停止: 'info',
    已完成: 'success',
    已启动: 'primary',
    未启动: 'info'
  }
  return types[label] || ''
}

// 执行状态标签
export const getLastExecutionLabel = (status) => {
  if (!status || status === 'pending') return '未执行'
  if (status === 'running') return '执行中'
  if (status === 'success') return '成功'
  if (status === 'failed' || status === 'cancelled') return '失败'
  return status
}

export const getExecutionTagType = (status) => {
  const label = getLastExecutionLabel(status)
  const types = {
    未执行: 'info',
    执行中: 'primary',
    成功: 'success',
    失败: 'danger'
  }
  return types[label] || 'info'
}

export const getExecutionLabel = (status) => {
  const label = getLastExecutionLabel(status)
  return label === '未执行' ? '待开始' : label
}

// 模式标签
export const getModeLabel = (mode) => {
  const labels = {
    batch: '批处理',
    stream: '流式',
    'micro-batch': '微批处理'
  }
  return labels[mode] || mode
}
