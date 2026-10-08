export { formatBytes } from '../../../basic/src/utils/formatters.js'

/**
 * 格式化日期时间
 */
export function formatDateTime(value) {
  if (!value) return '-'
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

/**
 * 格式化日期时间（别名）
 */
export const formatDate = formatDateTime

/**
 * 安全的 JSON 字符串化
 */
export function safeStringify(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch (error) {
    return String(value)
  }
}

/**
 * HTML 转义
 */
export function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * 格式化单元格值
 */
export function formatCellValue(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch (error) {
      return '[object]'
    }
  }
  return String(value)
}

/**
 * 获取对象节点类型标签
 */
export function getObjectNodeTypeLabel(type) {
  const key = String(type || '').toLowerCase()
  switch (key) {
    case 'directory':
    case 'prefix':
      return '目录'
    case 'bucket':
      return 'Bucket'
    case 'object':
      return '对象'
    default:
      return type || '-'
  }
}
