import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveExecutionMonitorRouteState } from '../src/utils/executionMonitorRouteState.js'

test('execution monitor route state keeps canonical filters and omits defaults', () => {
  assert.deepEqual(resolveExecutionMonitorRouteState({
    dev_type: 'workflow',
    status: 'failed',
    trigger_type: 'manual',
    source_task_id: '007',
    start_date: '2026-07-01',
    end_date: '2026-07-31',
    page: '1',
    page_size: '20'
  }), {
    filters: {
      dev_type: 'workflow',
      status: 'failed',
      trigger_type: 'manual',
      source_task_id: '7'
    },
    dateRange: ['2026-07-01', '2026-07-31'],
    page: 1,
    pageSize: 20,
    query: {
      dev_type: 'workflow',
      status: 'failed',
      trigger_type: 'manual',
      source_task_id: '7',
      start_date: '2026-07-01',
      end_date: '2026-07-31'
    },
    changed: true
  })
})

test('execution monitor route state removes unknown and invalid values', () => {
  assert.deepEqual(resolveExecutionMonitorRouteState({
    dev_type: 'legacy',
    status: 'done',
    trigger_type: 'cron',
    source_task_id: '-1',
    start_date: '2026-02-30',
    end_date: '2026-01-01',
    page: '0',
    page_size: '25',
    unknown: 'value'
  }), {
    filters: { dev_type: '', status: '', trigger_type: '', source_task_id: '' },
    dateRange: [],
    page: 1,
    pageSize: 20,
    query: {},
    changed: true
  })
})

test('execution monitor route state preserves non-default pagination', () => {
  assert.deepEqual(resolveExecutionMonitorRouteState({ page: '3', page_size: '50' }), {
    filters: { dev_type: '', status: '', trigger_type: '', source_task_id: '' },
    dateRange: [],
    page: 3,
    pageSize: 50,
    query: { page: '3', page_size: '50' },
    changed: false
  })
})
