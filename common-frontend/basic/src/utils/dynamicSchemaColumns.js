const normalizedPath = (metadata) => {
  if (Array.isArray(metadata?.path) && metadata.path.length > 0) {
    return metadata.path.map(segment => String(segment))
  }
  const name = String(metadata?.column_name || '').trim()
  return name ? name.split('.') : []
}

export const buildDynamicSchemaColumnDescriptors = (columns, columnMetadata) => {
  const metadata = Array.isArray(columnMetadata) ? columnMetadata : []
  return (Array.isArray(columns) ? columns : []).flatMap((column) => {
    const rootMetadata = metadata.find(item => {
      const path = normalizedPath(item)
      return path.length === 1 && path[0] === column
    })
    const objectColumn = String(rootMetadata?.type || '').toLowerCase() === 'object'
    if (!objectColumn) {
      return [{ key: column, label: column, path: [column] }]
    }

    const children = metadata
      .map(item => ({ item, path: normalizedPath(item) }))
      .filter(({ path }) => path.length === 2 && path[0] === column)
      .sort((left, right) => left.path[1].localeCompare(right.path[1]))
      .map(({ item, path }) => ({
        key: String(item.column_name || path.join('.')),
        label: String(item.column_name || path.join('.')),
        path
      }))

    return children.length > 0
      ? children
      : [{ key: column, label: column, path: [column] }]
  })
}

export const buildTablePreviewColumnDescriptors = ({
  columns,
  columnMetadata,
  geometryColumns,
  dynamicSchema = false
}) => {
  const descriptors = dynamicSchema
    ? buildDynamicSchemaColumnDescriptors(columns, columnMetadata)
    : (Array.isArray(columns) ? columns : []).map(column => ({
        key: column,
        label: column,
        path: [column]
      }))
  const geometrySet = new Set(Array.isArray(geometryColumns) ? geometryColumns : [])
  const filtered = descriptors.filter(column => !geometrySet.has(column.key))
  return filtered.length > 0 ? filtered : descriptors
}
