import { describe, expect, it } from 'vitest'
import {
  resolveEngineDetailRouteState,
  resolveIAMCategoryRouteState
} from '../src/utils/routeState'

describe('System recoverable route state', () => {
  it('falls back to the first permitted IAM tab and removes unrelated query state', () => {
    expect(resolveIAMCategoryRouteState(['memberships', 'invitations'], {
      tab: 'users',
      module_name: 'system'
    })).toEqual({
      activeTab: 'memberships',
      query: {},
      changed: true
    })
  })

  it('keeps only canonical audit filters and omits page one', () => {
    expect(resolveIAMCategoryRouteState(['account-security', 'audit'], {
      tab: 'audit',
      event_name: ' login ',
      result: 'succeeded',
      risk_level: 'invalid',
      module_name: ' system ',
      principal_id: ' 42 ',
      principal_type: 'user',
      entity_type: 'cleanup',
      entity_id: '42',
      page: '1',
      legacy: 'value'
    })).toEqual({
      activeTab: 'audit',
      query: {
        tab: 'audit',
        event_name: 'login',
        result: 'succeeded',
        module_name: 'system',
        principal_id: '42',
        principal_type: 'user',
        entity_type: 'cleanup',
        entity_id: '42'
      },
      changed: true
    })
  })

  it('drops unsupported audit principal types', () => {
    expect(resolveIAMCategoryRouteState(['audit'], { principal_type: 'application' })).toEqual({
      activeTab: 'audit',
      query: {},
      changed: true
    })
  })

  it('preserves a valid audit page and reports an already canonical route', () => {
    const query = { tab: 'audit', risk_level: 'high', page: '3' }
    expect(resolveIAMCategoryRouteState(['account-security', 'audit'], query)).toEqual({
      activeTab: 'audit',
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
