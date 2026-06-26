export const combinedMultiRefValue = '__combined__'

export function refDisplayName(path) {
  if (!path) return ''
  const parts = String(path).split(/[\\/]/).filter(Boolean)
  return parts.pop() || String(path)
}

export function multiPreviewRefs(previewData) {
  const contentRefs = previewData?.object?.content?.metadata?.refs
  const itemRefs = previewData?.object?.attributes?.item?.refs
  const refs = [
    ...(Array.isArray(contentRefs) ? contentRefs : []),
    ...(Array.isArray(itemRefs) ? itemRefs : [])
  ]
  const seen = new Set()
  return refs
    .filter(ref => ref && ref.path)
    .map((ref, index) => {
      const path = String(ref.path)
      const key = String(ref.key || ref.role || path || index)
      return {
        key,
        path,
        label: ref.label || ref.role || ref.key || refDisplayName(path) || String(index),
        role: ref.role || '',
        primary: Boolean(ref.primary),
        required: Boolean(ref.required),
        extension: ref.extension || ''
      }
    })
    .filter(ref => {
      if (seen.has(ref.path)) return false
      seen.add(ref.path)
      return true
    })
}

export function multiRefPreviewOptions(previewData, translate) {
  const refs = multiPreviewRefs(previewData)
    .map((ref, index) => {
      const fileName = refDisplayName(ref.path)
      return {
        ...ref,
        key: ref.key || ref.path || String(index),
        label: fileName && !String(ref.label).includes(fileName)
          ? `${ref.label} · ${fileName}`
          : String(ref.label)
      }
    })
  if (!refs.length) return []
  const t = typeof translate === 'function' ? translate : (key) => key
  return [
    {
      key: combinedMultiRefValue,
      path: combinedMultiRefValue,
      label: t('containerPreview.combinedPreview')
    },
    ...refs
  ]
}
