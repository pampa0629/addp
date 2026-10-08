/**
 * 格式化字节大小
 */
export function scaleByteValue(value) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) return null
  let bytes = value
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let index = 0
  while (Math.abs(bytes) >= 1024 && index < units.length - 1) {
    bytes /= 1024
    index++
  }
  return { value: bytes, unit: units[index], divisor: 1024 ** index }
}

export function formatBytes(value, precision = null, locale = 'zh-CN') {
  const numeric = typeof value === 'number' ? value : typeof value === 'string' && value.trim() ? Number(value) : NaN
  const scaled = scaleByteValue(numeric)
  if (!scaled) return '-'
  const bytes = scaled.value
  const digits = Number.isInteger(precision) ? precision : Math.abs(bytes) >= 100
    ? 0
    : Math.abs(bytes) >= 10 ? 1 : 2
  const formatted = new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(bytes)
  return `${formatted} ${scaled.unit}`
}

// Elapsed time, independent of time zones; does not represent a date or clock.
export function formatDurationSeconds(value, locale = 'zh-CN') {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > Number.MAX_SAFE_INTEGER) return '-'
  let remaining = Math.floor(value)
  const parts = []
  for (const [unit, size] of [['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1]]) {
    const count = Math.floor(remaining / size)
    remaining %= size
    if (count || (unit === 'second' && !parts.length)) parts.push(new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'long' }).format(count))
    if (parts.length === 3) break
  }
  return parts.join(' ')
}

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
 * @param {string} type - 节点类型
 * @param {Function} [t] - 可选的 i18n 翻译函数，传入时返回翻译文本
 */
export function getObjectNodeTypeLabel(type, t) {
  const key = String(type || '').toLowerCase()
  if (t) {
    switch (key) {
      case 'directory':
      case 'prefix':
        return t('objectStorage.typeDirectory')
      case 'bucket':
        return 'Bucket'
      case 'object':
        return t('objectStorage.typeObject')
      case 'schema':
        return 'Schema'
      case 'database':
        return t('objectStorage.typeDatabase')
      case 'table':
        return t('objectStorage.typeTable')
      case 'view':
        return t('objectStorage.typeView')
      default:
        return type || '-'
    }
  }
  switch (key) {
    case 'directory':
    case 'prefix':
      return '目录'
    case 'bucket':
      return 'Bucket'
    case 'object':
      return '对象'
    case 'schema':
      return 'Schema'
    case 'database':
      return '数据库'
    case 'table':
      return '数据表'
    case 'view':
      return '视图'
    default:
      return type || '-'
  }
}
