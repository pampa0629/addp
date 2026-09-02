export function exportFormatForRenderer(rendererType) {
  return rendererType === 'map' ? 'geojson' : 'csv'
}

export function descriptorSupportsExport(descriptor, rendererType) {
  return (descriptor?.input_contract?.formats || []).includes(exportFormatForRenderer(rendererType))
}

export function boundedExportHasMore(headers = {}) {
  return String(headers['x-addp-has-more'] || '').toLowerCase() === 'true'
}

export function downloadBoundedExport(data, filename) {
  const blob = data instanceof Blob ? data : new Blob([data])
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
