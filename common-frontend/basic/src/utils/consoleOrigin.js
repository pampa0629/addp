import { parseDevPorts } from './devPorts.js'

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

export function resolveConsoleOrigin(location, override = '', environment = import.meta.env) {
  if (hasValue(override)) return String(override).replace(/\/$/, '')
  if (!location?.origin) return ''
  const { protocol, hostname, port } = location
  const numericPort = Number.parseInt(port, 10)
  const registeredPorts = Object.values(parseDevPorts(environment?.VITE_ADDP_FRONTEND_PORTS))
  if (String(numericPort) === port &&
      ((numericPort >= 5173 && numericPort <= 5192) || registeredPorts.includes(port))) {
    return `${protocol}//${hostname}:${environment?.VITE_ADDP_CONSOLE_PORT || 5170}`
  }
  return location.origin
}
