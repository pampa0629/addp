export function isRuntimeInstanceOnline(instance, now = Date.now()) {
  const leaseExpiresAt = new Date(instance?.lease_expires_at).getTime()
  return instance?.status === 'up' && Number.isFinite(leaseExpiresAt) && leaseExpiresAt > now
}

export function isModuleRoutable(module, now = Date.now()) {
  return Boolean(module?.enabled && (module.instances || []).some(instance => (
    instance.role === 'backend' && instance.module_url && isRuntimeInstanceOnline(instance, now)
  )))
}
