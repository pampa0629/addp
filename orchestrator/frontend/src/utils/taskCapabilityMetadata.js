export function activeTaskCapabilityMetadata(taskCapabilities) {
  if (!Array.isArray(taskCapabilities)) return []
  return taskCapabilities
    .filter(item => typeof item?.type === 'string' && item.type.trim() !== '' && !item.deprecated)
    .map(item => ({
      type: item.type,
      editUrl: typeof item.edit_url === 'string' ? item.edit_url.trim() : ''
    }))
}
