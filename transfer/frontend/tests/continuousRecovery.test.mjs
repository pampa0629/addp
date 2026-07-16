import test from 'node:test'
import assert from 'node:assert/strict'

import {
	continuousRecoveryTagType,
	formatRecoverySeconds,
	getContinuousRecovery
} from '../../../common-frontend/basic/src/utils/continuousExecution.js'

const now = Date.parse('2026-07-15T08:00:00Z')

test('identifies persisted recovery waiting and circuit states', () => {
	const waiting = getContinuousRecovery({
		recovery_reason: 'lease_expired',
		recovery_attempt: 2,
		recovery_consecutive_failures: 2,
		recovery_backoff_seconds: 2,
		recovery_not_before: '2026-07-15T08:00:02Z',
		recovery_circuit_state: 'closed'
	}, 'pending', now)
	assert.equal(waiting.state, 'waiting')
	assert.equal(waiting.waitMilliseconds, 2000)
	assert.equal(waiting.consecutiveFailures, 2)

	const open = getContinuousRecovery({
		recovery_reason: 'execution_failed',
		recovery_attempt: 5,
		recovery_consecutive_failures: 5,
		recovery_not_before: '2026-07-15T08:05:00Z',
		recovery_circuit_state: 'open'
	}, 'pending', now)
	assert.equal(open.state, 'open')
	assert.equal(continuousRecoveryTagType(open.state), 'danger')
	assert.equal(getContinuousRecovery({
		recovery_reason: 'execution_failed',
		recovery_circuit_state: 'open'
	}, 'cancelled', now).state, 'completed')
})

test('shows half-open and running recovery sessions without inventing recovery metadata', () => {
	const halfOpen = getContinuousRecovery({
		recovery_reason: 'execution_failed',
		recovery_attempt: 5,
		recovery_consecutive_failures: 5,
		recovery_circuit_state: 'half_open'
	}, 'running', now)
	assert.equal(halfOpen.state, 'half_open')
	assert.equal(continuousRecoveryTagType(halfOpen.state), 'warning')

	const running = getContinuousRecovery({
		recovery_reason: 'worker_shutdown',
		recovery_attempt: 0,
		recovery_consecutive_failures: 0,
		recovery_circuit_state: 'closed'
	}, 'running', now)
	assert.equal(running.state, 'running')
	assert.equal(getContinuousRecovery({}, 'running', now), null)
})

test('formats recovery backoff durations', () => {
	assert.equal(formatRecoverySeconds(2), '2s')
	assert.equal(formatRecoverySeconds(90), '1.5m')
	assert.equal(formatRecoverySeconds(7200), '2h')
})
