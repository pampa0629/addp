import { describe, expect, it } from 'vitest'

import { resolveManagerTaskWorkspaceRouteState } from '@/utils/taskWorkspaceRoute'

describe('Manager task workspace recoverable route state', () => {
  it('omits the default tab and unknown query parameters', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: { tab: 'tasks', legacy: 'old' }
    })).toEqual({ tab: 'tasks', query: {}, changed: true })
  })

  it('preserves a result tab and canonical task filter', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: { tab: 'results', task_id: '007' },
      allowedQuery: ['task_id']
    })).toEqual({
      tab: 'results',
      query: { task_id: '7', tab: 'results' },
      changed: true
    })
  })

  it('keeps only validated creation context', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: {
        create: ['1', '0'],
        item_id: '12',
        locator: '  addp://engine/1/item/12  ',
        source_size_bytes: '2048',
        ignored: 'value'
      },
      allowedQuery: ['create', 'item_id', 'locator', 'source_size_bytes']
    })).toEqual({
      tab: 'tasks',
      query: {
        create: '1',
        item_id: '12',
        locator: 'addp://engine/1/item/12',
        source_size_bytes: '2048'
      },
      changed: true
    })
  })

  it('removes invalid IDs, create flags, and empty values', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: { task_id: '-1', item_id: 'abc', create: 'true', format: ' ' },
      allowedQuery: ['task_id', 'item_id', 'create', 'format']
    })).toEqual({ tab: 'tasks', query: {}, changed: true })
  })

  it('removes a result-only task filter from the task tab', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: { task_id: '7' },
      allowedQuery: ['task_id'],
      taskIDScope: 'results'
    })).toEqual({ tab: 'tasks', query: {}, changed: true })
  })

  it('uses tab-specific query allowlists', () => {
    expect(resolveManagerTaskWorkspaceRouteState({
      routeQuery: { tab: 'results', task_id: '7', create: '1' },
      allowedQueryByTab: {
        tasks: ['create', 'task_id'],
        results: ['task_id']
      }
    })).toEqual({
      tab: 'results',
      query: { task_id: '7', tab: 'results' },
      changed: true
    })
  })

  it('keeps canonical create and edit state on the task tab', () => {
    const options = {
      allowedQueryByTab: {
        tasks: ['create', 'task_id'],
        results: ['task_id']
      }
    }

    expect(resolveManagerTaskWorkspaceRouteState({
      ...options,
      routeQuery: { tab: 'tasks', create: '1' }
    })).toEqual({
      tab: 'tasks',
      query: { create: '1' },
      changed: true
    })
    expect(resolveManagerTaskWorkspaceRouteState({
      ...options,
      routeQuery: { task_id: '007' }
    })).toEqual({
      tab: 'tasks',
      query: { task_id: '7' },
      changed: true
    })
  })
})
