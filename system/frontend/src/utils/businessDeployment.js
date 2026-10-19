const fields = (value, keys) => value && typeof value === 'object' && !Array.isArray(value)
  && Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key))
const invalid = () => { throw new Error('invalid_business_deployment') }
export const isDeploymentUUID = value => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(value) && value !== '00000000-0000-0000-0000-000000000000'

// A local preview of the sole Business export protocol. System remains the
// authoritative validator; the file itself never establishes host identity.
export function parseBusinessDeployment(raw) {
  if (typeof raw !== 'string' || new TextEncoder().encode(raw).length > 16384) invalid()
  const value = JSON.parse(raw)
  if (!fields(value, ['schema', 'observed_at', 'source', 'services']) || value.schema !== 'addp.business-deployment-inventory/v1'
    || typeof value.observed_at !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/u.test(value.observed_at)
    || !Number.isFinite(Date.parse(value.observed_at)) || new Date(value.observed_at).toISOString() !== value.observed_at.replace('Z', '.000Z')
    || !fields(value.source, ['type', 'runtime', 'docker_engine_id', 'project']) || value.source.type !== 'docker_compose' || value.source.project !== 'business'
    || !['desktop_linux_vm', 'linux_docker'].includes(value.source.runtime) || typeof value.source.docker_engine_id !== 'string'
    || !/^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$/u.test(value.source.docker_engine_id) || !Array.isArray(value.services) || value.services.length !== 1) invalid()
  const service = value.services[0]
  if (!fields(service, ['service', 'engine_type', 'containers']) || service.service !== 'postgres' || service.engine_type !== 'postgresql'
    || !Array.isArray(service.containers) || service.containers.length !== 1) invalid()
  const container = service.containers[0]
  if (!fields(container, ['container_id', 'started_at', 'restart_count', 'published_ports']) || typeof container.container_id !== 'string'
    || !/^[0-9a-f]{64}$/u.test(container.container_id) || typeof container.started_at !== 'string'
    || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/u.test(container.started_at) || !Number.isFinite(Date.parse(container.started_at))
    || !Number.isSafeInteger(container.restart_count) || container.restart_count < 0 || !Array.isArray(container.published_ports)
    || !container.published_ports.length || container.published_ports.length > 8) invalid()
  for (const port of container.published_ports) {
    if (!fields(port, ['address', 'port', 'container_port', 'protocol']) || typeof port.address !== 'string' || !port.address
      || /[^0-9a-f:.]/iu.test(port.address) || !Number.isSafeInteger(port.port) || port.port < 1 || port.port > 65535 || port.container_port !== 5432 || port.protocol !== 'tcp') invalid()
  }
  return value
}
