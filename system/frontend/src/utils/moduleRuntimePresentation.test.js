import { describe, expect, it } from 'vitest'
import { getProcessUptimeParts, getRegisteredEndpoint } from './moduleRuntimePresentation'

describe('module runtime presentation', () => {
  it('shows the registered endpoint without pretending a hostname is a physical IP', () => {
    expect(getRegisteredEndpoint({ module_url: 'http://manager:8081/api' })).toBe('manager:8081')
    expect(getRegisteredEndpoint({ module_url: 'http://[::1]:8180' })).toBe('[::1]:8180')
    expect(getRegisteredEndpoint({ module_url: 'https://example.com' })).toBe('example.com:443')
    expect(getRegisteredEndpoint({ role: 'worker' })).toBeNull()
  })

  it('counts only the current online process uptime', () => {
    const now = Date.parse('2026-09-29T10:00:00Z')
    const instance = {
      status: 'up', lease_expires_at: '2026-09-29T10:00:10Z',
      process_started_at: '2026-09-28T08:57:38Z'
    }
    expect(getProcessUptimeParts(instance, now)).toEqual({ days: 1, hours: 1, minutes: 2, seconds: 22 })
    expect(getProcessUptimeParts({ ...instance, lease_expires_at: '2026-09-29T10:00:00Z' }, now)).toBeNull()
    expect(getProcessUptimeParts({ ...instance, status: 'down', stop_reason: 'graceful', stopped_at: '2026-09-29T09:00:00Z' }, now))
      .toEqual({ days: 1, hours: 0, minutes: 2, seconds: 22 })
    expect(getProcessUptimeParts({ ...instance, status: 'down', stop_reason: 'lease_expired', stopped_at: '2026-09-29T09:00:00Z' }, now)).toBeNull()
  })
})
