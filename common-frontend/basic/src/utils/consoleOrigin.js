function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

export function resolveConsoleOrigin(location, override = '') {
  if (hasValue(override)) return String(override).replace(/\/$/, '')
  if (!location?.origin) return ''
  const { protocol, hostname, port } = location
  const numericPort = Number.parseInt(port, 10)
  if (String(numericPort) === port && numericPort >= 5173 && numericPort <= 5190) {
    return `${protocol}//${hostname}:5170`
  }
  return location.origin
}
