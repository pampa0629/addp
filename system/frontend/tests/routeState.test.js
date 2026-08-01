import { describe, expect, it } from 'vitest'
import {
  resolveEngineDetailRouteState,
  resolveIAMRouteState
} from '../src/utils/routeState'

describe('System recoverable route state', () => {
  it('falls back to the first permitted IAM tab and removes unrelated query state', () => {
    expect(resolveIAMRouteState(['security', 'tenant-audit'], {
      tab: 'users',
      module_name: 'system'
    })).toEqual({
      activeTab: 'security',
      query: {},
      changed: true
    })
  })

  it('keeps only canonical audit filters and omits page one', () => {
    expect(resolveIAMRouteState(['security', 'tenant-audit'], {
      tab: 'tenant-audit',
      event_name: ' login ',
      result: 'succeeded',
      risk_level: 'invalid',
      module_name: ' system ',
      entity_type: 'cleanup',
      entity_id: '42',
      page: '1',
      legacy: 'value'
    })).toEqual({
      activeTab: 'tenant-audit',
      query: {
        tab: 'tenant-audit',
        event_name: 'login',
        result: 'succeeded',
        module_name: 'system',
        entity_type: 'cleanup',
        entity_id: '42'
      },
      changed: true
    })
  })

  it('preserves a valid audit page and reports an already canonical route', () => {
    const query = { tab: 'platform-audit', risk_level: 'high', page: '3' }
    expect(resolveIAMRouteState(['security', 'platform-audit'], query)).toEqual({
      activeTab: 'platform-audit',
      query,
      changed: false
    })
  })

  it('canonicalizes engine detail tabs against the loaded engine capabilities', () => {
    expect(resolveEngineDetailRouteState(['basic', 'connection'], { tab: 'connection' })).toEqual({
      activeTab: 'connection',
      query: { tab: 'connection' },
      changed: false
    })

    expect(resolveEngineDetailRouteState(['basic'], { tab: 'capabilities', legacy: '1' })).toEqual({
      activeTab: 'basic',
      query: {},
      changed: true
    })
  })
})
