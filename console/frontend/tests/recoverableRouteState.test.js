import { describe, expect, it } from 'vitest'

import { resolveCanonicalTabRouteState } from '../../../common-frontend/basic/src/utils/recoverableRouteState'
import { resolveExecutionDetailRouteState } from '../../../develop/frontend/src/utils/executionDetailRouteState'

describe('canonical recoverable tab state', () => {
  const resolve = (routeQuery, preservedQuery = {}) => resolveCanonicalTabRouteState({
    allowedTabs: ['basic', 'attributes', 'relations'],
    defaultTab: 'basic',
    routeQuery,
    preservedQuery
  })

  it('omits the default tab and removes unknown query parameters', () => {
    expect(resolve({})).toEqual({ tab: 'basic', query: {}, changed: false })
    expect(resolve({ tab: 'basic' })).toEqual({ tab: 'basic', query: {}, changed: true })
    expect(resolve({ tab: 'unknown', legacy: 'value' })).toEqual({
      tab: 'basic',
      query: {},
      changed: true
    })
  })

  it('preserves a valid non-default tab as the only tab representation', () => {
    expect(resolve({ tab: 'attributes' })).toEqual({
      tab: 'attributes',
      query: { tab: 'attributes' },
      changed: false
    })
    expect(resolve({ tab: ['relations', 'attributes'] })).toEqual({
      tab: 'relations',
      query: { tab: 'relations' },
      changed: true
    })
  })

  it('keeps only business-validated companion query state', () => {
    expect(resolve(
      { graph_id: '7', tab: 'attributes', legacy: 'value' },
      { graph_id: 7 }
    )).toEqual({
      tab: 'attributes',
      query: { graph_id: '7', tab: 'attributes' },
      changed: true
    })

    expect(resolve({ graph_id: '999' }, { graph_id: null })).toEqual({
      tab: 'basic',
      query: {},
      changed: true
    })
  })
})

describe('Develop execution detail recoverable route state', () => {
  it('allows the error tab only for failed executions', () => {
    expect(resolveExecutionDetailRouteState({ tab: 'error' }, 'failed')).toEqual({
      tab: 'error',
      query: { tab: 'error' },
      changed: false
    })
    expect(resolveExecutionDetailRouteState({ tab: 'error' }, 'success')).toEqual({
      tab: 'result',
      query: {},
      changed: true
    })
  })

  it('omits the default result tab and removes unknown query', () => {
    expect(resolveExecutionDetailRouteState({ tab: 'result', legacy: 'old' }, 'running')).toEqual({
      tab: 'result',
      query: {},
      changed: true
    })
  })
})
