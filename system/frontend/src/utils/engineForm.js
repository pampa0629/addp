export const switchStorageEngineType = (form, engineType) => ({
  ...form,
  engine_type: engineType,
  connection_info: {}
})

export const hasEngineAddressChanged = (descriptor, original = {}, updated = {}) =>
  (descriptor?.connection_spec?.fields || []).some(field => {
    if (!field.identity) return false
    const before = original[field.key] ?? field.default ?? ''
    const after = updated[field.key] ?? field.default ?? ''
    return String(before).trim() !== String(after).trim()
  })
