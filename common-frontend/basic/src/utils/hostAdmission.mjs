const invalid = () => { throw new Error('invalid_host_admission') }
const fields = (value, names) => value && typeof value === 'object' && !Array.isArray(value)
  && Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name))
const text = (value, limit) => typeof value === 'string' && value === value.trim() && value.length > 0
  && [...value].length <= limit && !/[\u0000-\u001f\u007f]/u.test(value)

// This is a proposal for a human to confirm, never an identity or authorization source.
export function parseHostAdmission(raw) {
  if (typeof raw !== 'string' || new TextEncoder().encode(raw).length > 16384) invalid()
  let value
  try { value = JSON.parse(raw) } catch { invalid() }
  if (!(fields(value, ['schema', 'host']) || fields(value, ['schema', 'host', 'collection'])) || value.schema !== 'addp.host-admission/v2') invalid()
  const { host, collection } = value
  if (!fields(host, ['display_name', 'node_kind', 'addresses', 'allowed_module_bindings'])
    || !text(host.display_name, 255) || !['physical', 'virtual'].includes(host.node_kind)
    || !Array.isArray(host.addresses) || host.addresses.length > 32 || new Set(host.addresses).size !== host.addresses.length
    || host.addresses.some(address => !text(address, 253) || /[\s/\\@?#"']/u.test(address))
    || !Array.isArray(host.allowed_module_bindings) || host.allowed_module_bindings.length > 64) invalid()
  const modules = new Set()
  for (const binding of host.allowed_module_bindings) {
    if (!fields(binding, ['module_name', 'client_id']) || typeof binding.module_name !== 'string'
      || !/^[a-z][a-z0-9_-]{0,49}$/u.test(binding.module_name) || binding.client_id !== `addp-${binding.module_name}`
      || modules.has(binding.module_name)) invalid()
    modules.add(binding.module_name)
  }
  if (!Object.hasOwn(value, 'collection')) return value
  validateHostCollection(collection)
  return value
}


export function validateHostCollection(collection) {
  if (!fields(collection, ['monitor_kind', 'source']) || collection.monitor_kind !== 'host_resources'
    || !fields(collection.source, ['type', 'endpoint']) || collection.source.type !== 'node_exporter'
    || !text(collection.source.endpoint, 512)) invalid()
  const match = /^https:\/\/(?:\[[0-9a-f:]+\]|[a-z0-9.-]+):([1-9][0-9]{0,4})\/metrics$/iu.exec(collection.source.endpoint)
  if (!match || Number(match[1]) > 65535) invalid()
  try { new URL(collection.source.endpoint) } catch { invalid() }
  return collection
}
