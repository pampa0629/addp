import { FieldType } from '../types/index.js'

const TYPE_ALIASES = new Map([
  ['char', FieldType.STRING],
  ['character', FieldType.STRING],
  ['clob', FieldType.STRING],
  ['nchar', FieldType.STRING],
  ['nvarchar', FieldType.STRING],
  ['text', FieldType.STRING],
  ['varchar', FieldType.STRING],
  ['bool', FieldType.BOOL],
  ['boolean', FieldType.BOOL],
  ['smallint', FieldType.INT],
  ['int', FieldType.INT],
  ['integer', FieldType.INT],
  ['int2', FieldType.INT],
  ['int4', FieldType.INT],
  ['bigint', FieldType.BIGINT],
  ['int8', FieldType.BIGINT],
  ['real', FieldType.FLOAT],
  ['float', FieldType.FLOAT],
  ['float4', FieldType.FLOAT],
  ['double', 'double'],
  ['double precision', 'double'],
  ['float8', 'double'],
  ['decimal', FieldType.DECIMAL],
  ['numeric', FieldType.DECIMAL],
  ['date', FieldType.DATE],
  ['time', FieldType.TIME],
  ['timestamp', FieldType.TIMESTAMP],
  ['timestamp without time zone', FieldType.TIMESTAMP],
  ['timestamp with time zone', FieldType.TIMESTAMP],
  ['timestamptz', FieldType.TIMESTAMP],
  ['json', FieldType.JSON],
  ['jsonb', FieldType.JSON],
  ['uuid', FieldType.UUID],
  ['bytea', FieldType.BYTES],
  ['binary', FieldType.BYTES],
  ['varbinary', FieldType.BYTES],
  ['blob', FieldType.BYTES],
  ['geometry', FieldType.GEOMETRY],
  ['geography', FieldType.GEOMETRY]
])

const SPATIAL_TYPE_RE = /^(?:geometry|geography)(?:\s*\(|$)/i

/**
 * 将数据源字段类型转换为 Transfer/format 约定的规范类型。
 *
 * 参数化数据库类型（如 GEOMETRY(Polygon, 32650)、DECIMAL(20,10)）的
 * 精度、空间类型和 SRID 等事实仍由字段元数据单独保留；这里仅负责类型
 * 选择，避免把数据库方言直接写进任务映射。
 */
export function normalizeFieldType(field, fallback = FieldType.STRING) {
  if (field && typeof field === 'object') {
    if (field.is_geometry === true || field.is_spatial === true || field.isSpatial === true) {
      return FieldType.GEOMETRY
    }
    field = field.type
  }

  const raw = String(field ?? '').trim()
  if (!raw) return fallback

  if (SPATIAL_TYPE_RE.test(raw)) return FieldType.GEOMETRY

  const normalized = raw.toLowerCase()
  const baseType = normalized.replace(/\s*\([^)]*\)\s*$/, '').trim()
  return TYPE_ALIASES.get(baseType) || normalized
}
