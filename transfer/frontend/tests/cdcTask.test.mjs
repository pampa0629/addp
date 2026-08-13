import test from 'node:test'
import assert from 'node:assert/strict'

import {
	buildCDCStopRequest,
	continuousStartDisabledReason,
	getCDCCaptureHealthWarning,
	getCDCSourceRecoveryWarning,
	isCDCSchemaBlocked,
	isDatabaseCDCTask
} from '../src/utils/cdcTask.mjs'

test('detects the provider-neutral database CDC task shape', () => {
  assert.equal(isDatabaseCDCTask({
    config: {
      runtime: { boundary: 'continuous' },
      load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
    }
  }), true)
  assert.equal(isDatabaseCDCTask({
    config: { runtime: { boundary: 'continuous' }, load: { mode: 'incremental', change_detection: { type: 'kafka' } } }
  }), false)
})

test('reports only confirmed critical source recovery loss', () => {
	const config = {
		runtime: { boundary: 'continuous' },
		load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
	}
	assert.deepEqual(getCDCSourceRecoveryWarning({
		config,
		capture: { source_recovery: { health: 'critical', capture_position: '100', earliest_available_position: '110' } }
	}), { capturePosition: '100', earliestAvailablePosition: '110' })
	assert.equal(getCDCSourceRecoveryWarning({ config, capture: { source_recovery: { health: 'unknown' } } }), null)
	assert.equal(getCDCSourceRecoveryWarning({ config, capture: { source_recovery: { health: 'healthy' } } }), null)
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
		capture: { status: 'failed', connector_status: 'RUNNING', source_status: 'OFFLINE' }
	}), { status: 'failed', connectorStatus: 'RUNNING', sourceStatus: 'OFFLINE' })
	assert.equal(getCDCCaptureHealthWarning({
		config,
		capture: { status: 'stopped', connector_status: 'DELETED' }
	}), null)
})

test('allows first database CDC start and distinguishes terminal capture state', () => {
	const config = {
		runtime: { boundary: 'continuous' },
		load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
	}
	assert.equal(continuousStartDisabledReason({ config, status: 'idle', desired_state: 'stopped' }), null)
	assert.equal(continuousStartDisabledReason({
		config,
		status: 'idle',
		desired_state: 'stopped',
		capture: { status: 'failed' }
	}), null)
	assert.equal(continuousStartDisabledReason({
		config,
		status: 'idle',
		desired_state: 'stopped',
		capture: { status: 'stopped' }
	}), 'permanently_stopped')
	assert.equal(continuousStartDisabledReason({
		config,
		status: 'idle',
		desired_state: 'stopped',
		capture: { status: 'cleanup_failed' }
	}), 'cleanup_failed')
})

test('reports other continuous start guards', () => {
	const config = {
		runtime: { boundary: 'continuous' },
		load: { mode: 'incremental', change_detection: { type: 'cdc', bootstrap: 'initial_snapshot' } }
	}
	assert.equal(continuousStartDisabledReason({ config, status: 'blocked', desired_state: 'paused' }), 'schema_blocked')
	assert.equal(continuousStartDisabledReason({ config, status: 'running', desired_state: 'paused' }), 'running')
	assert.equal(continuousStartDisabledReason({ config, status: 'idle', desired_state: 'running' }), 'running')
	assert.equal(continuousStartDisabledReason({ config, status: 'idle', desired_state: 'unknown' }), 'invalid_state')
})
