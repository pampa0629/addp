export const switchStorageEngineType = (form, engineType) => ({
  ...form,
  engine_type: engineType,
  connection_info: {}
})
