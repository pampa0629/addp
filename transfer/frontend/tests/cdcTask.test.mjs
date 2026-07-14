import test from 'node:test'
import assert from 'node:assert/strict'

import { buildCDCStopRequest, getCDCCaptureHealthWarning, isCDCSchemaBlocked, isPostgreSQLCDCTask } from '../src/utils/cdcTask.mjs'

test('detects only frozen PostgreSQL CDC task shape', () => {
  assert.equal(isPostgreSQLCDCTask({
    config: {
      runtime: { boundary: 'continuous' },
      load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
    }
  }), true)
  assert.equal(isPostgreSQLCDCTask({
    config: { runtime: { boundary: 'continuous' }, load: { mode: 'incremental', change_detection: { type: 'kafka' } } }
  }), false)
})

test('builds irreversible stop confirmation payload', () => {
  assert.deepEqual(buildCDCStopRequest('orders cdc', 'orders cdc'), {
    confirmed: true,
    confirmation_text: 'orders cdc'
  })
})

test('detects CDC schema-blocked task as non-resumable', () => {
	const task = {
		status: 'blocked',
		config: {
			runtime: { boundary: 'continuous' },
			load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
		}
	}
	assert.equal(isCDCSchemaBlocked(task), true)
	assert.equal(isCDCSchemaBlocked({ ...task, status: 'idle' }), false)
})

test('reports unhealthy active CDC capture without warning for stopped capture', () => {
	const config = {
		runtime: { boundary: 'continuous' },
		load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
	}
	assert.deepEqual(getCDCCaptureHealthWarning({
		config,
		capture: { status: 'failed', connector_status: 'FAILED' }
	}), { status: 'failed', connectorStatus: 'FAILED' })
	assert.equal(getCDCCaptureHealthWarning({
		config,
		capture: { status: 'stopped', connector_status: 'DELETED' }
	}), null)
})
