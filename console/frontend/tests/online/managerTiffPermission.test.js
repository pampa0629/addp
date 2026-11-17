import { describe, expect, it } from 'vitest'
import { matchesTiffPermissionResponse } from '../../e2e/online/manager-tiff-permission.js'

const target = { engine_id: 9, locator: 'addp://engine/9/path/private.tif?type=file&item_id=1' }
const response = (path, status, { method = 'GET', locator = target.locator, query = '' } = {}) => ({
  url: () => `http://console${path}?locator=${encodeURIComponent(locator)}${query}`,
  status: () => status, request: () => ({ method: () => method })
})

describe('Hosted TIFF permission observer exact response boundary', () => {
  it.each(['all_withdrawn', 'expired'])('accepts only the exact hidden ancestor denial in %s', phase => {
    const path = '/api/v1/meta/resource-tree/9/ancestors'
    expect(matchesTiffPermissionResponse(response(path, 404), target, phase)).toBe(true)
    for (const candidate of [response(path, 403), response(path, 500), response(path, 200),
      response('/api/v1/meta/resource-tree/10/ancestors', 404), response('/api/v1/manager/preview', 404),
      response(path, 404, { locator: 'other' }), response(path, 404, { method: 'POST' }),
      response(path, 404, { query: '&locator=other' })]) {
      expect(matchesTiffPermissionResponse(candidate, target, phase)).toBe(false)
    }
  })
  it.each(['metadata_only', 'read_withdrawn'])('requires a current preview response in %s', phase => {
    expect(matchesTiffPermissionResponse(response('/api/v1/manager/preview', 200), target, phase)).toBe(true)
    for (const candidate of [response('/api/v1/manager/preview', 404), response('/api/v1/manager/quick-view/capability', 200),
      response('/api/v1/manager/preview', 200, { locator: 'other' }), response('/api/v1/manager/preview', 200, { method: 'POST' })]) {
      expect(matchesTiffPermissionResponse(candidate, target, phase)).toBe(false)
    }
  })
})
