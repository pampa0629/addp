export function isRuntimeInstanceOnline(instance, now = Date.now()) {
  const leaseExpiresAt = new Date(instance?.lease_expires_at).getTime()
  return instance?.status === 'up' && Number.isFinite(leaseExpiresAt) && leaseExpiresAt > now
}

export function getModuleAvailability(module, now = Date.now()) {
  if (!module?.enabled) return 'disabled'

  const backendInstances = (module.instances || []).filter(instance => instance.role === 'backend')
  if (backendInstances.length === 0) return 'no_backend'

  const routable = backendInstances.some(instance => (
    instance.module_url && isRuntimeInstanceOnline(instance, now)
  ))
  return routable ? 'routable' : 'backend_offline'
}

export function isModuleRoutable(module, now = Date.now()) {
  return getModuleAvailability(module, now) === 'routable'
}
