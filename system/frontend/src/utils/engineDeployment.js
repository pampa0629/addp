const fields = (value, keys) => value && typeof value === 'object' && !Array.isArray(value)
  && Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key))
const invalid = () => { throw new Error('invalid_engine_deployment') }
export const isDeploymentUUID = value => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u.test(value) && value !== '00000000-0000-0000-0000-000000000000'

// Validate the canonical server declaration; it is not a live observation.
export function validateDeploymentInventory(value) {
  const raw = JSON.stringify(value)
  if (typeof raw !== 'string' || new TextEncoder().encode(raw).length > 16384) invalid()
  if (!fields(value, ['schema', 'observed_at', 'source', 'services']) || value.schema !== 'addp.engine-deployment-inventory/v1'
    || typeof value.observed_at !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/u.test(value.observed_at)
    || !Number.isFinite(Date.parse(value.observed_at)) || new Date(value.observed_at).toISOString() !== value.observed_at.replace('Z', '.000Z')
    || !fields(value.source, ['type', 'runtime', 'docker_engine_id', 'project']) || value.source.type !== 'docker_compose' || !['business', 'addp-infra'].includes(value.source.project)
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

export function validateDeploymentDiscovery(value) {
  if (!value || !Array.isArray(value.scopes) || value.scopes.length !== 2 || !Array.isArray(value.candidates) || value.candidates.length > 128) invalid()
  const seen = new Set()
  for (const scope of value.scopes) {
    if (!['infra','business'].includes(scope.scope) || seen.has(scope.scope) || !['available','unavailable','missing','unconfigured','stale'].includes(scope.state) || !Array.isArray(scope.services)) invalid()
    seen.add(scope.scope)
  }
  const keys = new Set()
  for (const item of value.candidates) {
    const scope = value.scopes.find(scope => scope.scope === item.scope)
    const source = scope?.source
    const service = scope?.services.find(service => service.service === item.service)
    const key = item.scope+'/'+item.service
    if (!source || scope.state !== 'available' || !service || keys.has(key) || typeof item.service !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/u.test(item.service) || !/^[a-f0-9]{64}$/u.test(item.fingerprint) || typeof item.eligible !== 'boolean' || (item.deployment_id && !isDeploymentUUID(item.deployment_id))) invalid()
    if (!item.eligible && !['unsupported_engine','unsupported_topology','not_running','sql_port_missing'].includes(item.reason)) invalid()
    if (item.eligible && (service.service !== 'postgres' || service.engine_type !== 'postgresql' || service.containers.length !== 1 || service.containers[0].status !== 'running')) invalid()
    keys.add(key)
  }
  return value
}

// Match current source facts to the declared physical source, never to an address.
export function resolveDeploymentSource(inventory, discovery, now = Date.now()) {
  validateDeploymentInventory(inventory)
  validateDeploymentDiscovery(discovery)
  const scope = discovery.scopes.find(item => item.scope === (inventory.source.project === 'business' ? 'business' : 'infra'))
  const unavailable = state => ({ state, observedAt: scope.observed_at || '', container: null, changed: false })
  if (scope.state !== 'available') return unavailable(scope.state)
  const sampled = Date.parse(scope.observed_at)
  if (!Number.isFinite(sampled) || sampled > now + 30000) invalid()
  if (sampled < now - 120000) return unavailable('stale')
  if (['type', 'runtime', 'docker_engine_id', 'project'].some(key => scope.source?.[key] !== inventory.source[key])) return unavailable('source_mismatch')
  const services = scope.services.filter(item => item.service === inventory.services[0].service)
  if (!services.length) return unavailable('not_found')
  if (services.length !== 1 || services[0].engine_type !== 'postgresql' || !Array.isArray(services[0].containers)) invalid()
  if (!services[0].containers.length) return unavailable('not_found')
  if (services[0].containers.length !== 1) return unavailable('unsupported_topology')
  const container = services[0].containers[0]
  if (!container || !/^[a-f0-9]{64}$/u.test(container.container_id) || !['created','running','paused','restarting','exited','dead','removing'].includes(container.status)
    || (container.started_at && !Number.isFinite(Date.parse(container.started_at))) || (container.status === 'running' && !container.started_at)
    || !Number.isSafeInteger(container.restart_count) || container.restart_count < 0) invalid()
  const original = inventory.services[0].containers[0]
  return { state: container.status, observedAt: scope.observed_at, container, changed: container.container_id !== original.container_id || container.started_at !== original.started_at }
}
