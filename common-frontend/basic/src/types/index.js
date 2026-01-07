/**
 * ADDP 前端共享类型定义
 */

/**
 * 预览数据结构
 * @typedef {Object} PreviewData
 * @property {Array<Object>} columns - 表格列定义
 * @property {Array<Object>} rows - 数据行
 * @property {number} total - 总记录数
 * @property {Object} [metadata] - 元数据信息
 */

/**
 * 字段类型枚举（对应后端 format.FieldType）
 */
export const FieldType = {
  STRING: 'string',
  INT: 'int',
  BIGINT: 'bigint',
  FLOAT: 'float',
  DECIMAL: 'decimal',
  BOOL: 'bool',
  DATE: 'date',
  TIME: 'time',
  TIMESTAMP: 'timestamp',
  BYTES: 'bytes',
  GEOMETRY: 'geometry',
  POINT: 'point',
  LINESTRING: 'linestring',
  POLYGON: 'polygon',
  MULTIPOINT: 'multipoint',
  JSON: 'json',
  ARRAY: 'array',
  UUID: 'uuid',
  UNKNOWN: 'unknown'
}

/**
 * 数据格式类型
 */
export const FormatType = {
  // 地理空间
  SHAPEFILE: 'shapefile',
  GEOJSON: 'geojson',
  GEOPACKAGE: 'geopackage',
  KML: 'kml',

  // 表格
  CSV: 'csv',
  EXCEL: 'excel',
  PARQUET: 'parquet',

  // 文档
  PDF: 'pdf',
  DOCX: 'docx',
  PPTX: 'pptx',
  MARKDOWN: 'markdown',

  // 图片
  IMAGE: 'image',
  GEOTIFF: 'geotiff',

  // 视频
  VIDEO: 'video',

  // 数据库
  POSTGRESQL: 'postgresql',
  MYSQL: 'mysql',
  SQLITE: 'sqlite',

  // 其他
  JSON: 'json',
  TEXT: 'text'
}

/**
 * 引擎类别类型（旧版本，重命名避免与 ResourceType 冲突）
 * @deprecated 请使用更明确的命名
 */
export const ResourceCategoryType = {
  DATABASE: 'database',
  OBJECT_STORAGE: 'object_storage',
  FILE_SYSTEM: 'file_system',
  API: 'api'
}

// 导出 ResourceLocator 相关工具
export {
  ResourceType,
  parseLocator,
  buildLocator,
  getPathString,
  getFullName,
  getLastSegment,
  getParentLocator,
  fromLegacyParams,
  cloneLocator,
  isLocatorEqual
} from './resourceLocator.js'
