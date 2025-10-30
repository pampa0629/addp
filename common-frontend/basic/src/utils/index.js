/**
 * ADDP 前端共享工具函数
 */

/**
 * 格式化文件大小
 * @param {number} bytes - 字节数
 * @returns {string} 格式化后的大小（如 "1.5 MB"）
 */
export function formatFileSize(bytes) {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i]
}

/**
 * 格式化日期时间
 * @param {string|Date} dateTime - 日期时间
 * @returns {string} 格式化后的日期时间
 */
export function formatDateTime(dateTime) {
  if (!dateTime) return '-'
  const date = new Date(dateTime)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

/**
 * 根据文件扩展名判断格式类型
 * @param {string} filename - 文件名
 * @returns {string} 格式类型
 */
export function detectFormatByExtension(filename) {
  if (!filename) return 'unknown'

  const ext = filename.toLowerCase().split('.').pop()

  // 地理空间格式
  if (['shp', 'shx', 'dbf', 'prj', 'cpg'].includes(ext)) return 'shapefile'
  if (['geojson', 'json'].includes(ext)) return 'geojson'
  if (['gpkg'].includes(ext)) return 'geopackage'
  if (['kml', 'kmz'].includes(ext)) return 'kml'

  // 表格格式
  if (['csv'].includes(ext)) return 'csv'
  if (['xlsx', 'xls'].includes(ext)) return 'excel'
  if (['parquet'].includes(ext)) return 'parquet'

  // 文档格式
  if (['pdf'].includes(ext)) return 'pdf'
  if (['docx', 'doc'].includes(ext)) return 'docx'
  if (['pptx', 'ppt'].includes(ext)) return 'pptx'
  if (['md', 'markdown'].includes(ext)) return 'markdown'

  // 图片格式
  if (['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'].includes(ext)) return 'image'
  if (['tif', 'tiff'].includes(ext)) return 'geotiff'

  // 视频格式
  if (['mp4', 'avi', 'mov', 'wmv', 'flv', 'mkv'].includes(ext)) return 'video'

  // 文本格式
  if (['txt', 'log'].includes(ext)) return 'text'

  return 'unknown'
}

/**
 * 格式化坐标
 * @param {number} coord - 坐标值
 * @param {number} precision - 精度（小数位数）
 * @returns {string} 格式化后的坐标
 */
export function formatCoordinate(coord, precision = 6) {
  if (coord === null || coord === undefined) return '-'
  return Number(coord).toFixed(precision)
}

/**
 * 判断是否为地理空间数据格式
 * @param {string} format - 格式类型
 * @returns {boolean}
 */
export function isGeospatialFormat(format) {
  const geospatialFormats = ['shapefile', 'geojson', 'geopackage', 'kml', 'geotiff']
  return geospatialFormats.includes(format)
}

/**
 * 判断是否为表格数据格式
 * @param {string} format - 格式类型
 * @returns {boolean}
 */
export function isTabularFormat(format) {
  const tabularFormats = ['csv', 'excel', 'parquet']
  return tabularFormats.includes(format)
}

/**
 * 判断是否为文档格式
 * @param {string} format - 格式类型
 * @returns {boolean}
 */
export function isDocumentFormat(format) {
  const documentFormats = ['pdf', 'docx', 'pptx', 'markdown']
  return documentFormats.includes(format)
}

/**
 * 判断是否为媒体格式
 * @param {string} format - 格式类型
 * @returns {boolean}
 */
export function isMediaFormat(format) {
  const mediaFormats = ['image', 'video', 'geotiff']
  return mediaFormats.includes(format)
}

/**
 * 获取字段类型的显示名称
 * @param {string} fieldType - 字段类型
 * @returns {string} 显示名称
 */
export function getFieldTypeLabel(fieldType) {
  const labels = {
    'string': '字符串',
    'int': '整数',
    'bigint': '长整数',
    'float': '浮点数',
    'decimal': '小数',
    'bool': '布尔值',
    'date': '日期',
    'time': '时间',
    'timestamp': '时间戳',
    'bytes': '二进制',
    'geometry': '几何',
    'point': '点',
    'linestring': '线',
    'polygon': '多边形',
    'multipoint': '多点',
    'json': 'JSON',
    'array': '数组',
    'uuid': 'UUID',
    'unknown': '未知'
  }
  return labels[fieldType] || fieldType
}

/**
 * 深拷贝对象
 * @param {any} obj - 待拷贝的对象
 * @returns {any} 拷贝后的对象
 */
export function deepClone(obj) {
  if (obj === null || typeof obj !== 'object') return obj
  if (obj instanceof Date) return new Date(obj)
  if (obj instanceof Array) return obj.map(item => deepClone(item))

  const clonedObj = {}
  for (const key in obj) {
    if (obj.hasOwnProperty(key)) {
      clonedObj[key] = deepClone(obj[key])
    }
  }
  return clonedObj
}

/**
 * 防抖函数
 * @param {Function} func - 待执行的函数
 * @param {number} wait - 等待时间（毫秒）
 * @returns {Function} 防抖后的函数
 */
export function debounce(func, wait = 300) {
  let timeout
  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout)
      func(...args)
    }
    clearTimeout(timeout)
    timeout = setTimeout(later, wait)
  }
}

/**
 * 节流函数
 * @param {Function} func - 待执行的函数
 * @param {number} limit - 限制时间（毫秒）
 * @returns {Function} 节流后的函数
 */
export function throttle(func, limit = 300) {
  let inThrottle
  return function executedFunction(...args) {
    if (!inThrottle) {
      func(...args)
      inThrottle = true
      setTimeout(() => (inThrottle = false), limit)
    }
  }
}
