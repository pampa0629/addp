import { describe, expect, it } from 'vitest'
import {
  resolveEngineDetailRouteState,
  resolveIAMCategoryRouteState,
  resolveModulesRouteState,
  buildModuleInstancesQuery
} from '../src/utils/routeState'

describe('System recoverable route state', () => {
  it('restores instance filters and omits defaults without leaking unrelated parameters', () => {
    const state = resolveModulesRouteState({
      tab: 'instances', module_name: ' manager ', registered_host: ' host-a ', node_name: 'container-a',
      role: 'worker', status: 'up', time_period: '15m', page: '02', page_size: '20', token: 'must-not-persist'
    })
    expect(state.query).toEqual({ tab: 'instances', module_name: 'manager', registered_host: 'host-a',
      node_name: 'container-a', role: 'worker', time_period: '15m', page: '2', page_size: '20' })
    expect(state.filters).toMatchObject({ moduleName: 'manager', status: 'up', page: 2, pageSize: 20 })
    expect(state.changed).toBe(true)
    expect(resolveModulesRouteState(state.query).changed).toBe(false)
    expect(resolveModulesRouteState({}).changed).toBe(false)
  })

  it('restores node IP independently of endpoints and node names', () => {
    const query = { tab: 'instances', node_ip: '2001:db8::1', node_name: 'host-a', registered_host: 'service.local' }
    const state = resolveModulesRouteState(query)
    expect(state.filters).toMatchObject({ nodeIP: '2001:db8::1', nodeName: 'host-a', registeredHost: 'service.local' })
    expect(buildModuleInstancesQuery(state.filters)).toEqual(query)
    expect(resolveModulesRouteState({ tab: 'instances', node_ip: ['192.0.2.1', '192.0.2.2'] }).query).toEqual({ tab: 'instances' })
  })

  it('keeps an explicit all-status query distinct from the UP default', () => {
    const state = resolveModulesRouteState({ tab: 'instances', status: 'all' })
    expect(state.filters.status).toBe('')
    expect(buildModuleInstancesQuery(state.filters)).toEqual({ tab: 'instances', status: 'all' })
    expect(buildModuleInstancesQuery(resolveModulesRouteState({ tab: 'instances' }).filters)).toEqual({ tab: 'instances' })
  })

  it('restores offline time queries and removes retired registration-only parameters', () => {
    const query = { tab: 'instances', status: 'down', time_basis: 'offline', time_period: '1h' }
    const state = resolveModulesRouteState(query)
    expect(state.filters).toMatchObject({ timeBasis: 'offline', period: '1h', status: 'down' })
    expect(buildModuleInstancesQuery(state.filters)).toEqual(query)
    expect(state.changed).toBe(false)
    expect(resolveModulesRouteState({ tab: 'instances', time_basis: 'registered' }).query).toEqual({ tab: 'instances' })
    expect(resolveModulesRouteState({ tab: 'instances', time_basis: ['offline', 'registered'] }).query).toEqual({ tab: 'instances' })
    expect(resolveModulesRouteState({ tab: 'instances', time_basis: 'started',
      registered_period: 'custom', registered_from: '2026-10-01T00:00:00Z', registered_to: '2026-10-02T00:00:00Z'
    }).query).toEqual({ tab: 'instances' })
  })

  it('restores stop reasons without changing status or time basis and removes invalid reasons', () => {
    for (const stopReason of ['graceful', 'lease_expired']) {
      const query = { tab: 'instances', status: 'all', stop_reason: stopReason, time_basis: 'offline', page: '2' }
      const state = resolveModulesRouteState(query)
      expect(state.filters).toMatchObject({ stopReason, status: '', timeBasis: 'offline', page: 2 })
      expect(buildModuleInstancesQuery(state.filters)).toEqual(query)
      expect(state.changed).toBe(false)
    }
    expect(resolveModulesRouteState({ tab: 'instances', stop_reason: 'graceful' }).filters.status).toBe('up')
    for (const stop_reason of ['unknown', ['graceful', 'lease_expired'], '']) {
      expect(resolveModulesRouteState({ tab: 'instances', stop_reason }).query).toEqual({ tab: 'instances' })
    }
  })

  it('canonicalizes complete custom ranges to UTC and discards incomplete or inverted ranges', () => {
    const state = resolveModulesRouteState({ tab: 'instances', time_period: 'custom',
      time_from: '2026-10-01T08:00:00+08:00', time_to: '2026-10-01T09:00:00+08:00' })
    expect(state.query).toEqual({ tab: 'instances', time_period: 'custom',
      time_from: '2026-10-01T00:00:00.000Z', time_to: '2026-10-01T01:00:00.000Z' })
    for (const range of [
      { time_from: 'invalid', time_to: '2026-10-01T00:00:00Z' },
      { time_from: '2026-02-30T00:00:00Z', time_to: '2026-10-01T00:00:00Z' },
      { time_from: '2026-10-01T00:00:00Z' },
      { time_from: '2026-10-02T00:00:00Z', time_to: '2026-10-01T00:00:00Z' }
    ]) expect(resolveModulesRouteState({ tab: 'instances', time_period: 'custom', ...range }).query).toEqual({ tab: 'instances' })
  })

  it('removes invalid enums, repeated fields, unsafe pages and unsupported page sizes', () => {
    expect(resolveModulesRouteState({ tab: 'instances', module_name: ['manager', 'system'], status: 'bad',
      role: 'other', time_period: 'bad', page: '9007199254740992', page_size: '11' }).query).toEqual({ tab: 'instances' })
    expect(resolveModulesRouteState({ tab: ['instances', 'overview'], module_name: 'manager' }).query).toEqual({})
    expect(resolveModulesRouteState({ tab: 'overview', status: 'down', page: '2' }).query).toEqual({})
  })
  it('falls back to the first permitted IAM tab and removes unrelated query state', () => {
    expect(resolveIAMCategoryRouteState(['user-accounts', 'invitations'], {
      tab: 'users',
      module_name: 'system'
    })).toEqual({
      activeTab: 'user-accounts',
      query: {},
      changed: true
    })
  })

  it('removes the retired mixed machine-identity tab instead of keeping a compatibility route', () => {
    expect(resolveIAMCategoryRouteState([
      'api-consumers',
      'oauth-clients',
      'tenant-service-accounts',
      'platform-runtime-accounts'
    ], { tab: 'service-accounts' })).toEqual({
      activeTab: 'api-consumers',
      query: {},
      changed: true
    })
  })

  it('keeps only canonical audit filters and omits page one', () => {
    expect(resolveIAMCategoryRouteState(['security-policy', 'audit'], {
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
    expect(resolveIAMCategoryRouteState(['security-policy', 'audit'], query)).toEqual({
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
