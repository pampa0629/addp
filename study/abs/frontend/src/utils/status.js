const STATUS_LABELS = {
  pending: '排队中',
  processing: '处理中',
  compiling: '编译中',
  deploying: '部署中',
  completed: '已完成',
  failed: '失败'
}

export const getStatusLabel = (status) => STATUS_LABELS[status] || status
