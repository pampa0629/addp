import { isRuntimeInstanceOnline } from './moduleRegistry'

export function getRegisteredEndpoint(instance) {
  const address = instance?.module_url || instance?.health_check_url
  if (!address) return null
  try {
    const url = new URL(address)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    const port = url.port || (url.protocol === 'https:' ? '443' : '80')
    return `${url.hostname}:${port}`
  } catch {
    return null
  }
}

export function getProcessUptimeParts(instance, now = Date.now()) {
  if (!instance?.process_started_at) return null
  const startedAt = new Date(instance.process_started_at).getTime()
  const endedAt = isRuntimeInstanceOnline(instance, now)
    ? now
    : instance?.status === 'down' && instance?.stop_reason === 'graceful' && instance?.stopped_at
      ? new Date(instance.stopped_at).getTime()
      : NaN
  if (!Number.isFinite(startedAt) || !Number.isFinite(endedAt) || endedAt < startedAt) return null
  const totalSeconds = Math.floor((endedAt - startedAt) / 1000)
  return {
    days: Math.floor(totalSeconds / 86400),
    hours: Math.floor(totalSeconds % 86400 / 3600),
    minutes: Math.floor(totalSeconds % 3600 / 60),
    seconds: totalSeconds % 60
  }
}

export function formatRuntimeUptime(instance, t, now = Date.now()) {
  const parts = getProcessUptimeParts(instance, now)
  if (!parts) return '—'
  if (parts.days) return t('system.module.instances.uptimeDays', parts)
  if (parts.hours) return t('system.module.instances.uptimeHours', parts)
  if (parts.minutes) return t('system.module.instances.uptimeMinutes', parts)
  return t('system.module.instances.uptimeSeconds', parts)
}
