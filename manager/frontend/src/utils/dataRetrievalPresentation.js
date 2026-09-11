export function retrievalResultPath(item = {}, formatLocator) {
  const indexedPath = item.full_name || item.path || item.relative_path
  if (indexedPath) return indexedPath
  return formatLocator?.(item.locator) || ''
}
