import { describe, expect, it } from 'vitest'
import { getModuleAvailability, isModuleRoutable, isRuntimeInstanceOnline, moduleNeedsAttention } from './moduleRegistry'

const now = Date.parse('2026-08-22T10:00:00Z')

describe('module registry projections', () => {
  it('treats only up instances with a valid lease as online', () => {
    expect(isRuntimeInstanceOnline({ status: 'up', lease_expires_at: '2026-08-22T10:00:01Z' }, now)).toBe(true)
    expect(isRuntimeInstanceOnline({ status: 'up', lease_expires_at: '2026-08-22T10:00:00Z' }, now)).toBe(false)
    expect(isRuntimeInstanceOnline({ status: 'down', lease_expires_at: '2026-08-22T10:01:00Z' }, now)).toBe(false)
  })

  it('requires an enabled module and a valid backend URL', () => {
    const backend = {
      role: 'backend',
      status: 'up',
      module_url: 'http://manager:8081',
      lease_expires_at: '2026-08-22T10:00:30Z'
    }
    expect(isModuleRoutable({ enabled: true, instances: [backend] }, now)).toBe(true)
    expect(isModuleRoutable({ enabled: false, instances: [backend] }, now)).toBe(false)
    expect(isModuleRoutable({ enabled: true, instances: [{ ...backend, role: 'worker' }] }, now)).toBe(false)
    expect(isModuleRoutable({ enabled: true, instances: [{ ...backend, module_url: '' }] }, now)).toBe(false)
  })

  it('distinguishes administrator intent from runtime availability', () => {
    const backend = {
      role: 'backend',
      status: 'up',
      module_url: 'http://manager:8081',
      lease_expires_at: '2026-08-22T10:00:30Z'
    }

    expect(getModuleAvailability({ enabled: false, instances: [backend] }, now)).toBe('disabled')
    expect(getModuleAvailability({ enabled: true, instances: [] }, now)).toBe('no_backend')
    expect(getModuleAvailability({
      enabled: true,
      instances: [{ ...backend, lease_expires_at: '2026-08-22T10:00:00Z' }]
    }, now)).toBe('backend_offline')
    expect(getModuleAvailability({ enabled: true, instances: [backend] }, now)).toBe('routable')
  })

  it('shows Gateway ingress health without treating it as a route target', () => {
    const ingress = {
      role: 'ingress', status: 'up', module_url: 'http://gateway:8000',
      lease_expires_at: '2026-08-22T10:00:30Z'
    }
    const gateway = { module_name: 'gateway', enabled: true, instances: [ingress] }
    expect(getModuleAvailability(gateway, now)).toBe('ingress_online')
    expect(isModuleRoutable(gateway, now)).toBe(false)
    expect(moduleNeedsAttention(gateway, now)).toBe(false)
    expect(moduleNeedsAttention({ ...gateway, instances: [{ ...ingress, lease_expires_at: '2026-08-22T10:00:00Z' }] }, now)).toBe(true)
  })

  it('surfaces enabled modules without a backend and expired instances', () => {
    expect(moduleNeedsAttention({ enabled: true, instances: [] }, now)).toBe(true)
    expect(moduleNeedsAttention({ enabled: false, instances: [] }, now)).toBe(false)
    expect(moduleNeedsAttention({ enabled: true, instances: [{
      role: 'worker', status: 'down', stop_reason: 'lease_expired',
      lease_expires_at: '2026-08-22T09:59:00Z'
    }] }, now)).toBe(true)
  })
})
