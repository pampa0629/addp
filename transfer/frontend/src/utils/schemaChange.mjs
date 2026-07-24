export function buildSchemaChangeApproval(fields) {
  if (!Array.isArray(fields) || fields.length === 0) return null
  const normalized = fields.map((field) => ({
    source: String(field?.source || '').trim(),
    target: String(field?.target || '').trim(),
    target_type: String(field?.target_type || '').trim(),
    nullable: field?.nullable === true
  }))
  if (normalized.some((field) => !field.source || !field.target || !field.target_type || !field.nullable)) {
    return null
  }
  if (new Set(normalized.map((field) => field.source)).size !== normalized.length) return null
  if (new Set(normalized.map((field) => field.target)).size !== normalized.length) return null
  return { fields: normalized }
}
