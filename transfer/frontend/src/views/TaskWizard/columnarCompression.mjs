export function normalizeColumnarCompressionCapability(raw) {
  if (!raw || typeof raw !== 'object') return null

  const codecs = Array.isArray(raw.codecs)
    ? raw.codecs
        .map(codec => String(codec || '').trim())
        .filter(codec => codec && codec === codec.toLowerCase())
        .filter((codec, index, values) => values.indexOf(codec) === index)
    : []
  const defaultCodec = String(raw.default || '').trim()
  if (!defaultCodec || defaultCodec !== defaultCodec.toLowerCase() || !codecs.includes(defaultCodec)) {
    return null
  }
  return { codecs, default: defaultCodec }
}

export function resolveColumnarCompression(capability, selected = '') {
  if (!capability) return ''
  const value = String(selected || '').trim()
  if (!value) return capability.default
  return capability.codecs.includes(value) ? value : ''
}

export function withColumnarCompressionOption(baseOptions, capability, selected = '') {
  const options = { ...(baseOptions || {}) }
  if (!capability) return options

  const compression = resolveColumnarCompression(capability, selected)
  if (compression) options.compression = compression
  return options
}
