export const resolvePositiveRouteId = value => {
  const id = Number(value)
  return Number.isInteger(id) && id > 0 ? id : null
}

export const isEditableDraft = (status, canUpdate) => status === 'draft' && Boolean(canUpdate)

export const buildDDLPreviewRequest = materialization => ({
  materialization: {
    schema_name: String(materialization?.schema_name || '').trim(),
    table_name: String(materialization?.table_name || '').trim(),
    partition_by: String(materialization?.partition_by || '').trim(),
    partition_type: String(materialization?.partition_type || '').trim().toLowerCase()
  }
})
